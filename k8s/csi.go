package k8s

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"sync/atomic"
	"time"

	gimporter "github.com/PlakarKorp/integration-grpc/importer"
	"github.com/PlakarKorp/kloset/connectors"
	"github.com/PlakarKorp/kloset/connectors/importer"
	"github.com/PlakarKorp/kloset/location"
	vs "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

func isready(snap *vs.VolumeSnapshot) bool {
	return snap.Status != nil && snap.Status.ReadyToUse != nil && *snap.Status.ReadyToUse
}

func (k *k8s) gensnap(ctx context.Context, ns, name string) (*vs.VolumeSnapshot, error) {
	log.Println(">>>> in gensnap for", ns, name)
	snap := &vs.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "snap-" + name + "-",
			Namespace:    ns,
			Labels: map[string]string{
				"plakar.io/generated-resource": "true",
			},
		},
		Spec: vs.VolumeSnapshotSpec{
			Source: vs.VolumeSnapshotSource{
				PersistentVolumeClaimName: &name,
			},
			VolumeSnapshotClassName: &k.volumeSnapshotClassName,
		},
	}

	snap, err := k.snapClient.SnapshotV1().VolumeSnapshots(ns).Create(ctx, snap,
		metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	log.Println("created", snap.Name)

	w, err := k.snapClient.SnapshotV1().VolumeSnapshots(ns).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		k.delsnap(ctx, snap)
		return nil, err
	}

	defer w.Stop()
	for {
		var evt watch.Event
		var ok bool
		select {
		case evt, ok = <-w.ResultChan():
			if !ok {
				return snap, err
			}
		case <-ctx.Done():
			k.delsnap(ctx, snap)
			return nil, ctx.Err()
		}

		if evt.Type == watch.Error {
			k.delsnap(ctx, snap)
			return nil, fmt.Errorf("watch failed")
		}

		if evt.Type != watch.Modified {
			continue
		}

		s, ok := evt.Object.(*vs.VolumeSnapshot)
		if !ok {
			log.Printf("the watcher returned an object of an unknown type %t",
				evt.Object)
			continue
		}

		if s.Name != snap.Name {
			continue
		}

		if s.Status != nil && s.Status.Error != nil && s.Status.Error.Message != nil {
			k.delsnap(ctx, s)
			return nil, fmt.Errorf("%s", *s.Status.Error.Message)
		}

		if isready(s) {
			snap = s
			log.Printf("the snapshot %s is ready!", snap.Name)
			break
		}
	}

	return snap, err
}

func (k *k8s) delsnap(ctx context.Context, snap *vs.VolumeSnapshot) error {
	log.Println("deleting snap", snap.Name)
	return k.snapClient.SnapshotV1().VolumeSnapshots(snap.ObjectMeta.Namespace).
		Delete(ctx, snap.ObjectMeta.Name, metav1.DeleteOptions{})
}

func (k *k8s) pvcFromSnap(ctx context.Context, ns string, snap *vs.VolumeSnapshot) (*corev1.PersistentVolumeClaim, error) {
	apiGroup := "snapshot.storage.k8s.io"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "from-snap-",
			Namespace:    ns,
			Labels: map[string]string{
				"plakar.io/generated-resource": "true",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snap.Name,
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("1Gi"),
				},
			},
		},
	}

	return k.clientset.CoreV1().PersistentVolumeClaims(ns).
		Create(ctx, pvc, metav1.CreateOptions{})
}

func (k *k8s) delpvc(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	log.Println("deleting pvc", pvc.Name)
	return k.clientset.CoreV1().PersistentVolumeClaims(pvc.ObjectMeta.Namespace).
		Delete(ctx, pvc.Name, metav1.DeleteOptions{})
}

func (k *k8s) fsServer(ctx context.Context, ns string, pvc *corev1.PersistentVolumeClaim) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "plakar-backup-",
			Namespace:    ns,
			Labels: map[string]string{
				"plakar.io/generated-resource": "true",
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				Name: "snap",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc.Name,
						ReadOnly:  true,
					},
				},
			}},
			Containers: []corev1.Container{{
				Name:  "kubelet",
				Image: k.kubeletImage,
				Args:  []string{"-p", "8080"},
				Ports: []corev1.ContainerPort{{
					Name:          "grpc",
					ContainerPort: 8080,
				}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "snap",
					MountPath: "/data",
				}},
			}},
		},
	}

	pod, err := k.clientset.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	log.Println("pod created", pod.Name)

	w, err := k.clientset.CoreV1().Pods(pod.Namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		k.delpod(ctx, pod)
		return nil, err
	}

	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			k.delpod(ctx, pod)
			return nil, ctx.Err()

		case evt, ok := <-w.ResultChan():
			if !ok {
				return pod, nil
			}

			if evt.Type == watch.Error {
				k.delpod(ctx, pod)
				return nil, fmt.Errorf("watch failed")
			}

			if evt.Type != watch.Modified {
				continue
			}

			p, ok := evt.Object.(*corev1.Pod)
			if !ok {
				log.Printf("pods: the watcher returned an object of type %t",
					evt.Object)
				continue
			}

			if p.Name != pod.Name {
				continue
			}

			if len(p.Status.ContainerStatuses) > 0 && p.Status.ContainerStatuses[0].Ready {
				return p, nil
			}
		}
	}
}

func (k *k8s) delpod(ctx context.Context, pod *corev1.Pod) error {
	log.Println("deleting pod", pod.Name)
	return k.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
}

func progress(ctx context.Context, imp importer.Importer, fn func(<-chan *connectors.Record, chan<- *connectors.Result)) error {
	var (
		size    = 2
		records = make(chan *connectors.Record, size)
		retch   = make(chan struct{}, 1)
	)

	var results chan *connectors.Result
	if (imp.Flags() & location.FLAG_NEEDACK) != 0 {
		results = make(chan *connectors.Result, size)
	}

	go func() {
		fn(records, results)
		if results != nil {
			close(results)
		}
		close(retch)
	}()

	err := imp.Import(ctx, records, results)
	<-retch
	return err
}

func (k *k8s) consume(ctx context.Context, dest, podpath string, pvc *corev1.PersistentVolumeClaim, inflight *inflight, Records chan<- *connectors.Record) (int64, error) {
	client, err := grpc.NewClient(dest, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, fmt.Errorf("failed to create a grpc client for %s: %w", dest, err)
	}

	opts := &connectors.Options{
		Hostname:        "foobar",
		OperatingSystem: "linux",
		Architecture:    "amd64",
		CWD:             podpath,
		MaxConcurrency:  2,
	}

	importer, err := gimporter.NewImporter(ctx, client, opts, "fs", map[string]string{
		"location":         "fs://" + podpath,
		"dont_traverse_fs": "true",
	})
	if err != nil {
		return 0, fmt.Errorf("failed to instantiate the importer: %w", err)
	}

	inflight.mtx.Lock()
	inflight.inflight[pvc.Namespace+"/"+pvc.Name] = new(atomic.Int64)
	inflight.mtx.Unlock()

	var n int64
	err = progress(ctx, importer, func(records <-chan *connectors.Record, results chan<- *connectors.Result) {
		for record := range records {
			if record.Pathname == "/" {
				if results != nil {
					results <- record.Ok()
				} else {
					record.Close()
				}
				continue
			}

			n++

			newrecord := *record
			newrecord.Pathname = path.Join("/data/pvc/", pvc.Namespace, pvc.Name, record.Pathname)
			Records <- &newrecord
		}
	})
	return n, err
}

func (k *k8s) fsBackup(ctx context.Context, pod *corev1.Pod, pvc *corev1.PersistentVolumeClaim, inflight *inflight, records chan<- *connectors.Record) error {
	var url string
	if k.portForward {
		u := k.clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(pod.Namespace).
			Name(pod.Name).
			SubResource("portforward").URL()

		transport, upgrader, err := spdy.RoundTripperFor(k.config)
		if err != nil {
			return err
		}

		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", u)

		var (
			stopChan  = make(chan struct{}, 1)
			readyChan = make(chan struct{}, 1)
		)

		defer close(stopChan)

		pf, err := portforward.New(dialer, []string{":8080"}, stopChan, readyChan, io.Discard, io.Discard)
		if err != nil {
			return err
		}

		go pf.ForwardPorts()

		<-readyChan
		ports, err := pf.GetPorts()
		if err != nil {
			return err
		}

		url = fmt.Sprintf("localhost:%d", ports[0].Local)
	} else {
		url = fmt.Sprintf("%s.%s.svc.cluster.local:8080", pod.Name, pod.Namespace)
	}

	log.Println("url is", url)

	expected, err := k.consume(ctx, url, "/data", pvc, inflight, records)
	if err != nil {
		log.Println("failed to run the grpc importer:", err)
		return err
	}

	inflight.mtx.RLock()
	done := inflight.inflight[pvc.Namespace+"/"+pvc.Name]
	inflight.mtx.RUnlock()

	for {
		if expected == done.Load() {
			break
		}

		time.Sleep(time.Second)
	}

	return nil
}

func (k *k8s) backupPvc(ctx context.Context, ns, name string, inflight *inflight, records chan<- *connectors.Record) error {
	snap, err := k.gensnap(ctx, ns, name)
	if err != nil {
		log.Println("failed to generate the snapshot:", err)
		return err
	}

	pvc, err := k.pvcFromSnap(ctx, ns, snap)
	if err != nil {
		k.delsnap(ctx, snap)
		log.Println("failed to generate the pvc from the snap:", err)
		return err
	}

	pod, err := k.fsServer(ctx, ns, pvc)
	if err != nil {
		k.delpvc(ctx, pvc)
		k.delsnap(ctx, snap)
		log.Println("failed to generate pod from the pvc:", err)
		return err
	}

	err = k.fsBackup(ctx, pod, pvc, inflight, records)
	if err != nil {
		log.Println("failed to backup the pod:", err)
	}

	if err := k.delpod(ctx, pod); err != nil {
		log.Printf("failed to delete pod %s/%s: %s", pod.ObjectMeta.Namespace,
			pvc.ObjectMeta.Name, err)
	}

	if err := k.delpvc(ctx, pvc); err != nil {
		log.Printf("failed to delete PVC %s/%s: %s", pvc.ObjectMeta.Namespace,
			pvc.ObjectMeta.Name, err)
	}

	if err := k.delsnap(ctx, snap); err != nil {
		log.Printf("failed to delete VolumeSnapshot %s/%s: %s", snap.ObjectMeta.Namespace,
			snap.ObjectMeta.Name, err)
	}

	return err
}
