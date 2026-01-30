package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	annotationKey = "tcpdump.antrea.io"
	captureDir    = "/"
	hostProcPath  = "/host/proc"
)

type CaptureManager struct {
	mu       sync.Mutex
	captures map[string]context.CancelFunc
}

func NewCaptureManager() *CaptureManager {
	return &CaptureManager{
		captures: make(map[string]context.CancelFunc),
	}
}

func (cm *CaptureManager) StartCapture(pod *corev1.Pod, n int, nodeName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	uid := string(pod.UID)
	if _, exists := cm.captures[uid]; exists {
		return
	}

	log.Printf("Starting capture for pod %s/%s with rotation %d", pod.Namespace, pod.Name, n)

	pid, err := findPid(pod)
	if err != nil {
		log.Printf("Error finding PID for pod %s: %v", pod.Name, err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cm.captures[uid] = cancel

	go func() {
		pcapFile := fmt.Sprintf("%scapture-%s.pcap", captureDir, pod.Name)

		cmd := exec.CommandContext(ctx, "nsenter", "-t", strconv.Itoa(pid), "-n", "--",
			"tcpdump", "-Z", "root", "-C", "1", "-W", strconv.Itoa(n), "-w", pcapFile)

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		log.Printf("Running: %s", cmd.String())
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.Canceled {
				log.Printf("Capture stopped for pod %s", pod.Name)
			} else {
				log.Printf("tcpdump for pod %s failed: %v", pod.Name, err)
			}
		}
	}()
}

func (cm *CaptureManager) StopCapture(pod *corev1.Pod) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	uid := string(pod.UID)
	if cancel, exists := cm.captures[uid]; exists {
		log.Printf("Stopping capture for pod %s", pod.Name)
		cancel()
		delete(cm.captures, uid)

		pattern := fmt.Sprintf("%scapture-%s.pcap*", captureDir, pod.Name)
		files, err := filepath.Glob(pattern)
		if err == nil {
			for _, f := range files {
				os.Remove(f)
				log.Printf("Deleted file %s", f)
			}
		}
	}
}

func findPid(pod *corev1.Pod) (int, error) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return 0, fmt.Errorf("no container statuses")
	}
	
	containerID := pod.Status.ContainerStatuses[0].ContainerID
	parts := strings.Split(containerID, "://")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid container ID: %s", containerID)
	}
	id := parts[1]

	entries, err := os.ReadDir(hostProcPath)
	if err != nil {
		return 0, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		cgroupPath := filepath.Join(hostProcPath, pidStr, "cgroup")
		content, err := os.ReadFile(cgroupPath)
		if err != nil {
			continue
		}

		if strings.Contains(string(content), id) {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("pid not found")
}

func main() {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatal("NODE_NAME environment variable not set")
	}

	log.Printf("Starting PacketCapture controller on node %s", nodeName)

	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("Error building kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building clientset: %v", err)
	}

	cm := NewCaptureManager()

	lw := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"pods",
		corev1.NamespaceAll,
		fields.OneTermEqualSelector("spec.nodeName", nodeName),
	)

	informer := cache.NewSharedInformer(lw, &corev1.Pod{}, 0)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			handlePod(pod, cm, nodeName)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			handlePod(pod, cm, nodeName)
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				pod, ok = tombstone.Obj.(*corev1.Pod)
				if !ok {
					return
				}
			}
			cm.StopCapture(pod)
		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)
	go informer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
		log.Fatal("Timed out waiting for caches to sync")
	}

	log.Println("Controller started, watching pods...")
	select {}
}

func handlePod(pod *corev1.Pod, cm *CaptureManager, nodeName string) {
	val, ok := pod.Annotations[annotationKey]
	if !ok {
		cm.StopCapture(pod)
		return
	}

	n, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("Invalid annotation value for pod %s: %s", pod.Name, val)
		return
	}

	cm.StartCapture(pod, n, nodeName)
}
