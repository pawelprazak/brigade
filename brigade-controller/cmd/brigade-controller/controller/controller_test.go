package controller

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
)

func TestController(t *testing.T) {
	createdPod := false
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		createdPod = true
		t.Log("creating pod")
		return false, nil, nil
	})
	svcAccountName := "my-service-account"
	config := &Config{
		Namespace:            v1.NamespaceDefault,
		WorkerImage:          "deis/brigade-worker:latest",
		WorkerPullPolicy:     string(v1.PullIfNotPresent),
		WorkerServiceAccount: svcAccountName,
	}
	controller := NewController(client, config)

	secret := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "moby",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "build",
				"project":   "ahab",
				"build":     "queequeg",
			},
		},
	}

	sidecarImage := "fake/sidecar:latest"
	project := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "ahab",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "project",
			},
		},
		// This and the missing 'script' will trigger an initContainer
		Data: map[string][]byte{
			"vcsSidecar": []byte(sidecarImage),
		},
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&secret)
	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&project)

	// Let's wait for the controller to create the pod
	wait.Poll(100*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return createdPod, nil
	})

	pod, err := client.CoreV1().Pods(v1.NamespaceDefault).Get(secret.Name, meta.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !labels.Equals(pod.GetLabels(), secret.GetLabels()) {
		t.Error("Pod.Lables do not match")
	}

	if pod.Spec.ServiceAccountName != svcAccountName {
		t.Errorf("expected service account %s, got %s", svcAccountName, pod.Spec.ServiceAccountName)
	}

	if pod.Spec.Volumes[0].Name != volumeName {
		t.Error("Spec.Volumes are not correct")
	}

	c := pod.Spec.Containers[0]
	if c.Name != "brigade-runner" {
		t.Error("Container.Name is not correct")
	}
	if envlen := len(c.Env); envlen != 13 {
		t.Errorf("expected 13 items in Container.Env, got %d", envlen)
	}
	if c.Image != config.WorkerImage {
		t.Error("Container.Image is not correct")
	}

	for i, term := range []string{"yarn", "-s", "start"} {
		if c.Command[i] != term {
			t.Errorf("Expected command %d to be %q, got %q", i, term, c.Command[i])
		}
	}

	if c.VolumeMounts[0].Name != volumeName {
		t.Error("Container.VolumeMounts is not correct")
	}

	if l := len(pod.Spec.InitContainers); l != 1 {
		t.Fatalf("Expected 1 init container, got %d", l)
	}
	ic := pod.Spec.InitContainers[0]
	if envlen := len(ic.Env); envlen != 13 {
		t.Errorf("expected 13 env vars, got %d", envlen)
	}

	if ic.Image != sidecarImage {
		t.Errorf("expected sidecar %q, got %q", sidecarImage, ic.Image)
	}

	if ic.VolumeMounts[0].Name != sidecarVolumeName {
		t.Errorf("expected sidecar volume %q, got %q", sidecarVolumeName, ic.VolumeMounts[0].Name)
	}

	if os, ok := pod.Spec.NodeSelector["beta.kubernetes.io/os"]; !ok {
		t.Error("No OS node selector found")
	} else if os != "linux" {
		t.Errorf("Unexpected node selector: %s", os)
	}
}

func TestController_WithScript(t *testing.T) {
	createdPod := false
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		createdPod = true
		t.Log("creating pod")
		return false, nil, nil
	})

	config := &Config{
		Namespace:        v1.NamespaceDefault,
		WorkerImage:      "deis/brigade-worker:latest",
		WorkerPullPolicy: string(v1.PullIfNotPresent),
	}
	controller := NewController(client, config)

	secret := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "moby",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "build",
				"project":   "ahab",
				"build":     "queequeg",
			},
		},
		Data: map[string][]byte{
			"script": []byte("hello"),
		},
	}

	sidecarImage := "fake/sidecar:latest"
	project := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "ahab",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "project",
			},
		},
		// This and the missing 'script' will trigger an initContainer
		Data: map[string][]byte{
			"vcsSidecar": []byte(sidecarImage),
		},
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&secret)
	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&project)

	// Let's wait for the controller to create the pod
	wait.Poll(100*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return createdPod, nil
	})

	pod, err := client.CoreV1().Pods(v1.NamespaceDefault).Get(secret.Name, meta.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !labels.Equals(pod.GetLabels(), secret.GetLabels()) {
		t.Error("Pod.Lables do not match")
	}

	if pod.Spec.Volumes[0].Name != volumeName {
		t.Error("Spec.Volumes are not correct")
	}
	c := pod.Spec.Containers[0]
	if c.Name != "brigade-runner" {
		t.Error("Container.Name is not correct")
	}
	if envlen := len(c.Env); envlen != 13 {
		t.Errorf("expected 13 items in Container.Env, got %d", envlen)
	}
	if c.Image != config.WorkerImage {
		t.Error("Container.Image is not correct")
	}
	if c.VolumeMounts[0].Name != volumeName {
		t.Error("Container.VolumeMounts is not correct")
	}

	if l := len(pod.Spec.InitContainers); l != 0 {
		t.Fatalf("Expected no init container, got %d", l)
	}
}

func TestController_NoSidecar(t *testing.T) {
	createdPod := false
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		createdPod = true
		t.Log("creating pod")
		return false, nil, nil
	})

	config := &Config{
		Namespace:        v1.NamespaceDefault,
		WorkerImage:      "deis/brigade-worker:latest",
		WorkerPullPolicy: string(v1.PullIfNotPresent),
	}
	controller := NewController(client, config)

	secret := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "moby",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "build",
				"project":   "ahab",
				"build":     "queequeg",
			},
		},
	}

	project := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "ahab",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "project",
			},
		},
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&secret)
	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&project)

	// Let's wait for the controller to create the pod
	wait.Poll(100*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return createdPod, nil
	})

	pod, err := client.CoreV1().Pods(v1.NamespaceDefault).Get(secret.Name, meta.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := pod.Spec.Containers[0]
	if envlen := len(c.Env); envlen != 13 {
		t.Errorf("expected 13 items in Container.Env, got %d", envlen)
	}
	if c.Image != config.WorkerImage {
		t.Error("Container.Image is not correct")
	}
	if l := len(pod.Spec.InitContainers); l != 0 {
		t.Fatalf("Expected no init container, got %d", l)
	}
}

func TestController_WithWorkerCommand(t *testing.T) {
	createdPod := false
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		createdPod = true
		t.Log("creating pod")
		return false, nil, nil
	})

	config := &Config{
		Namespace:        v1.NamespaceDefault,
		WorkerImage:      "deis/brigade-worker:latest",
		WorkerPullPolicy: string(v1.PullIfNotPresent),
	}
	controller := NewController(client, config)

	secret := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "moby",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "build",
				"project":   "ahab",
				"build":     "queequeg",
			},
		},
		Data: map[string][]byte{
			"script": []byte("hello"),
		},
	}

	sidecarImage := "fake/sidecar:latest"
	project := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "ahab",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "project",
			},
		},
		// This and the missing 'script' will trigger an initContainer
		Data: map[string][]byte{
			"vcsSidecar":    []byte(sidecarImage),
			"workerCommand": []byte("worker command"),
		},
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&secret)
	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&project)

	// Let's wait for the controller to create the pod
	wait.Poll(100*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return createdPod, nil
	})

	pod, err := client.CoreV1().Pods(v1.NamespaceDefault).Get(secret.Name, meta.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := pod.Spec.Containers[0]
	if c.Name != "brigade-runner" {
		t.Error("Container.Name is not correct")
	}
	for i, term := range []string{"worker", "command"} {
		if c.Command[i] != term {
			t.Errorf("Expected command %d to be %q, got %q", i, term, c.Command[i])
		}
	}
}

func TestController_WithProjectSpecificWorkerConfig(t *testing.T) {
	createdPod := false
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		createdPod = true
		t.Log("creating pod")
		return false, nil, nil
	})
	config := &Config{
		Namespace:        v1.NamespaceDefault,
		WorkerImage:      "deis/brigade-worker:latest",
		WorkerPullPolicy: string(v1.PullIfNotPresent),
	}
	controller := NewController(client, config)

	secret := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "moby",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "build",
				"project":   "ahab",
				"build":     "queequeg",
			},
		},
	}

	sidecarImage := "fake/sidecar:latest"
	workerRegistry := "myrepo"
	workerName := "brigade-worker-with-deps"
	workerTag := "canary"
	workerPullPolicy := v1.PullPolicy("Always")
	workerImage := fmt.Sprintf("%s/%s:%s", workerRegistry, workerName, workerTag)
	project := v1.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "ahab",
			Namespace: v1.NamespaceDefault,
			Labels: map[string]string{
				"heritage":  "brigade",
				"component": "project",
			},
		},
		// This and the missing 'script' will trigger an initContainer
		Data: map[string][]byte{
			"vcsSidecar":        []byte(sidecarImage),
			"worker.registry":   []byte(workerRegistry),
			"worker.name":       []byte(workerName),
			"worker.tag":        []byte(workerTag),
			"worker.pullPolicy": []byte(workerPullPolicy),
		},
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&secret)
	client.CoreV1().Secrets(v1.NamespaceDefault).Create(&project)

	// Let's wait for the controller to create the pod
	wait.Poll(100*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return createdPod, nil
	})

	pod, err := client.CoreV1().Pods(v1.NamespaceDefault).Get(secret.Name, meta.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !labels.Equals(pod.GetLabels(), secret.GetLabels()) {
		t.Error("Pod.Lables do not match")
	}

	if pod.Spec.Volumes[0].Name != volumeName {
		t.Error("Spec.Volumes are not correct")
	}

	c := pod.Spec.Containers[0]
	if c.Name != "brigade-runner" {
		t.Error("Container.Name is not correct")
	}
	if envlen := len(c.Env); envlen != 13 {
		t.Errorf("expected 13 items in Container.Env, got %d", envlen)
	}
	if c.Image != workerImage {
		t.Error("Container.Image is not correct")
	}
	if c.ImagePullPolicy != workerPullPolicy {
		t.Error("Container.ImagePullPolicy is not correct")
	}
	if c.VolumeMounts[0].Name != volumeName {
		t.Error("Container.VolumeMounts is not correct")
	}

	if l := len(pod.Spec.InitContainers); l != 1 {
		t.Fatalf("Expected 1 init container, got %d", l)
	}
	ic := pod.Spec.InitContainers[0]
	if envlen := len(ic.Env); envlen != 13 {
		t.Errorf("expected 13 env vars, got %d", envlen)
	}

	if ic.Image != sidecarImage {
		t.Errorf("expected sidecar %q, got %q", sidecarImage, ic.Image)
	}

	if ic.ImagePullPolicy != workerPullPolicy {
		t.Errorf("expected sidecar %q, got %q", workerPullPolicy, ic.ImagePullPolicy)
	}

	if ic.VolumeMounts[0].Name != sidecarVolumeName {
		t.Errorf("expected sidecar volume %q, got %q", sidecarVolumeName, ic.VolumeMounts[0].Name)
	}

	if os, ok := pod.Spec.NodeSelector["beta.kubernetes.io/os"]; !ok {
		t.Error("No OS node selector found")
	} else if os != "linux" {
		t.Errorf("Unexpected node selector: %s", os)
	}
}
