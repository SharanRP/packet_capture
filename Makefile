.PHONY: help build-image kind-load deploy-controller deploy-test verify-capture clean logs

BINARY_NAME=simple-packet-capture
IMAGE_NAME=simple-packet-capture
IMAGE_TAG=latest
IMAGE=$(IMAGE_NAME):$(IMAGE_TAG)
NAMESPACE=kube-system
POD_NAME=test-pod

help:
	@echo "Antrea PacketCapture Controller - Available targets:"
	@echo ""
	@echo "Build & Deploy:"
	@echo "  make build-image      - Build Docker image"
	@echo "  make kind-load        - Load image into Kind cluster"
	@echo "  make build            - Build image and load to Kind"
	@echo "  make deploy-controller - Deploy RBAC and DaemonSet"
	@echo "  make deploy-test      - Deploy test Pod"
	@echo "  make deploy-all       - Deploy controller, RBAC, and test Pod"
	@echo ""
	@echo "Operations:"
	@echo "  make start-capture    - Annotate test pod to start capture"
	@echo "  make stop-capture     - Remove annotation to stop capture"
	@echo "  make verify-capture   - Check if capture is running"
	@echo "  make logs             - Tail logs from capture pod"
	@echo "  make extract-pcap     - Extract pcap file from cluster"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean            - Delete controller and test pod"
	@echo "  make clean-all        - Delete everything"
	@echo ""

build-binary:
	@go build -o $(BINARY_NAME) .

build-image: build-binary
	@docker build -t $(IMAGE) .
	@rm $(BINARY_NAME)

kind-load: build-image
	@kind load docker-image $(IMAGE)

build: kind-load

deploy-rbac:
	@kubectl apply -f manifests/rbac.yaml

deploy-controller: deploy-rbac
	@kubectl apply -f manifests/daemonset.yaml
	@kubectl wait --for=condition=ready pod -l app=packet-capture -n $(NAMESPACE) --timeout=30s 2>/dev/null || true
	@sleep 2

deploy-test:
	@kubectl apply -f manifests/test-pod.yaml
	@kubectl wait --for=condition=ready pod test-pod --timeout=30s 2>/dev/null || true

deploy-all: deploy-controller deploy-test

start-capture:
	@kubectl annotate pod $(POD_NAME) tcpdump.antrea.io="5" --overwrite
	@sleep 2
	@$(MAKE) verify-capture

stop-capture:
	@kubectl annotate pod $(POD_NAME) tcpdump.antrea.io- 2>/dev/null || true
	@sleep 2
	@CAPTURE_POD=$$(kubectl get pods -n $(NAMESPACE) -l app=packet-capture -o jsonpath="{.items[0].metadata.name}"); \
	if [ -n "$$CAPTURE_POD" ]; then \
		kubectl exec -n $(NAMESPACE) $$CAPTURE_POD -- bash -c "ls /capture-$(POD_NAME).pcap* 2>&1" || echo "Files deleted"; \
	fi

verify-capture:
	@CAPTURE_POD=$$(kubectl get pods -n $(NAMESPACE) -l app=packet-capture -o jsonpath="{.items[0].metadata.name}"); \
	if [ -z "$$CAPTURE_POD" ]; then \
		echo "No capture pod found"; exit 1; \
	fi; \
	echo "Capture pod: $$CAPTURE_POD"; \
	echo ""; \
	echo "Pcap files:"; \
	kubectl exec -n $(NAMESPACE) $$CAPTURE_POD -- ls -lh /capture-$(POD_NAME).pcap* 2>/dev/null || echo "No files yet"; \
	echo ""; \
	echo "Recent logs:"; \
	kubectl logs -n $(NAMESPACE) $$CAPTURE_POD --tail=5

extract-pcap:
	@CAPTURE_POD=$$(kubectl get pods -n $(NAMESPACE) -l app=packet-capture -o jsonpath="{.items[0].metadata.name}"); \
	if [ -z "$$CAPTURE_POD" ]; then \
		echo "No capture pod found"; exit 1; \
	fi; \
	kubectl cp $(NAMESPACE)/$$CAPTURE_POD:/capture-$(POD_NAME).pcap0 ./$(POD_NAME).pcap && \
	echo "File extracted: $(POD_NAME).pcap" || echo "Copy failed"

logs:
	@CAPTURE_POD=$$(kubectl get pods -n $(NAMESPACE) -l app=packet-capture -o jsonpath="{.items[0].metadata.name}"); \
	if [ -z "$$CAPTURE_POD" ]; then \
		echo "No capture pod found"; exit 1; \
	fi; \
	kubectl logs -n $(NAMESPACE) $$CAPTURE_POD -f

status:
	@echo "Capture pods:"; \
	kubectl get pods -n $(NAMESPACE) -l app=packet-capture 2>/dev/null || echo "None found"; \
	echo ""; \
	echo "Test pod:"; \
	kubectl get pod $(POD_NAME) 2>/dev/null || echo "Not found"

clean:
	@kubectl delete daemonset -n $(NAMESPACE) packet-capture 2>/dev/null || true
	@kubectl delete pod $(POD_NAME) 2>/dev/null || true

clean-all: clean
	@kubectl delete clusterrolebinding packet-capture-binding 2>/dev/null || true
	@kubectl delete clusterrole packet-capture-role 2>/dev/null || true
	@kubectl delete serviceaccount -n $(NAMESPACE) packet-capture-sa 2>/dev/null || true
	@docker rmi $(IMAGE) 2>/dev/null || true

.PHONY: help build-image build kind-load deploy-rbac deploy-controller deploy-test deploy-all start-capture stop-capture verify-capture extract-pcap logs status clean clean-all build-binary
