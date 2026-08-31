# Intel TDX Enablement on prod-cluster-1

> **Historical environment proposal.** This planning record is not part of
> the portable llm-d-fleet contract and is excluded from the OSS source
> archive.

**Date**: 2026-07-08
**Status**: Planning — requires prod-cluster-1 infrastructure team coordination
**Prerequisite**: BIOS access (BMC/IPMI) to worker nodes

---

## What is TDX

Intel Trust Domain Extensions (TDX) provides hardware-isolated VMs ("Trust Domains") where the CPU encrypts memory so that even the hypervisor, VMM, and host admin cannot access the data inside. For CPU inference, this means:

- **Model weights encrypted in hardware** — not visible to cloud operators
- **User prompts and responses never leave the trust boundary**
- **Remote attestation** proves the trust domain is genuine before sending data

## prod-cluster-1 Hardware Status

| Requirement | Status | Details |
|------------|--------|---------|
| **CPU** | Ready | Intel Xeon 6767P (Granite Rapids) — TDX supported since 5th Gen Xeon. `tme` flag present in cpuinfo. |
| **BIOS TDX** | Not enabled | No TDX labels on nodes. BIOS configuration needed. |
| **Kernel params** | Not set | `kvm_intel.tdx=1` not in kernel cmdline |
| **OpenShift version** | Ready | 4.18.36 — meets 4.18+ requirement for confidential containers |
| **Confidential Containers operator** | Not installed | Available in OperatorHub |

## Steps to Enable

### Step 1: BIOS Configuration (Infrastructure Team)

Requires BMC/IPMI or physical access to each worker node where TDX is desired.

Navigate to: **Socket Configuration → Processor Configuration → TME, TME-MT, TDX**

| Setting | Value |
|---------|-------|
| Total Memory Encryption (TME) | Enable |
| Total Memory Encryption Multi-Tenant (TME-MT) | Enable |
| TDX Secure Arbitration Mode Loader (SEAM Loader) | Enable |
| Intel TDX | Enable |
| TDX Key Split | Set (allocate KeyIDs for TDX, e.g., 16-64) |
| Software Guard Extension (SGX) | Enable (optional, for attestation) |

**Reboot required** after BIOS changes.

Start with 1-2 worker nodes (e.g., worker01-02) to minimize blast radius. Don't enable on all nodes simultaneously.

### Step 2: Kernel Parameters (OpenShift MachineConfig)

**WARNING**: Applying a MachineConfig reboots all nodes with the matching role label. Use a custom label to target specific nodes.

Option A: Target specific nodes via label:
```bash
# Label only TDX-target nodes
oc label node ocp-rac-maas-worker01 node-role.kubernetes.io/tdx-worker=
oc label node ocp-rac-maas-worker02 node-role.kubernetes.io/tdx-worker=
```

```yaml
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 99-tdx-kernel-params
  labels:
    machineconfiguration.openshift.io/role: tdx-worker
spec:
  kernelArguments:
    - kvm_intel.tdx=1
```

Option B: Apply to all workers (reboots all 9 workers):
```yaml
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 99-tdx-kernel-params
  labels:
    machineconfiguration.openshift.io/role: worker
spec:
  kernelArguments:
    - kvm_intel.tdx=1
```

### Step 3: Verify TDX is Active

```bash
# Check kernel messages
oc debug node/ocp-rac-maas-worker01 -- dmesg | grep tdx
# Expected: virt/tdx: BIOS enabled: private KeyID range: [16, 64)

# Check node labels (NFD should auto-detect)
oc get nodes -l feature.node.kubernetes.io/cpu-security.tdx.enabled=true

# Check TDX module loaded
oc debug node/ocp-rac-maas-worker01 -- ls /sys/firmware/tdx/
```

### Step 4: Install Confidential Containers Operator

Via OperatorHub:
1. Navigate to Operators → OperatorHub
2. Search "OpenShift sandboxed containers"
3. Install with default settings
4. Create KataConfig:

```yaml
apiVersion: kataconfiguration.openshift.io/v1
kind: KataConfig
metadata:
  name: tdx-kata-config
spec:
  kataConfigPoolSelector:
    matchLabels:
      node-role.kubernetes.io/tdx-worker: ""
  enablePeerPods: false
```

### Step 5: Deploy Trustee for Attestation

Trustee provides the Key Broker Service (KBS) for remote attestation:

```bash
# Install Trustee operator via OperatorHub
# Then create attestation policy
```

### Step 6: Run Confidential AI Inference

Deploy a CPU inference pod with the `kata-tdx` runtime class:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: intel-confidential-inference
  labels:
    fleet.llm-d.ai/hardware: cpu-intel-tdx
    intel.ai/confidential: "true"
spec:
  runtimeClassName: kata-tdx
  containers:
    - name: serve
      image: quay.io/fleet-llm-d/fleet-controller:intel-cpu-inference
      env:
        - name: MODEL_NAME
          value: granite-2b-cpu
      # ... same config as non-confidential pod
```

### Step 7: Benchmark TD vs Non-TD

Run inference harness against:
- Non-TD pod (regular container) — baseline
- TD pod (kata-tdx runtime) — confidential

Expected overhead: 5-15% for memory encryption.

## Risks

| Risk | Mitigation |
|------|------------|
| BIOS change bricks a node | Start with 1 non-critical worker. Have BMC recovery access. |
| MachineConfig reboots all workers | Use targeted label (Option A) to limit to 2 nodes |
| TDX performance overhead | Benchmark first — 5-15% expected, may be acceptable |
| Trustee/attestation complexity | Start without attestation — just prove TD isolation works |
| BIOS doesn't support TDX on this motherboard | Check with OEM before attempting — Xeon 6767P CPU supports it, but board BIOS must too |

## Timeline Estimate

| Step | Effort | Dependency |
|------|--------|------------|
| BIOS config | 30 min per node | Infrastructure team + maintenance window |
| MachineConfig | 15 min + reboot | BIOS done first |
| Verify | 15 min | Reboot complete |
| Operator install | 30 min | Verification done |
| First TD pod | 1 hour | Operator ready |
| Benchmark | 1 hour | TD pod running |
| **Total** | **~4 hours** | **Needs maintenance window for reboots** |

## Rollback

```bash
# Remove kernel params (triggers reboot)
oc delete machineconfig 99-tdx-kernel-params

# Remove operator
oc delete kataconfig tdx-kata-config
oc delete subscription sandboxed-containers-operator -n openshift-sandboxed-containers-operator

# BIOS: revert TDX settings in BIOS setup (requires BMC access)
```

## References

- [Setting up Intel TDX VMs with Trustee on OpenShift](https://developers.redhat.com/articles/2025/11/05/setting-intel-tdx-vms-trustee-openshift)
- [Confidential Containers on Bare Metal](https://developers.redhat.com/articles/2025/02/19/how-deploy-confidential-containers-bare-metal)
- [Intel TDX Overview](https://www.intel.com/content/www/us/en/developer/tools/trust-domain-extensions/overview.html)
- [Linux Kernel TDX Documentation](https://docs.kernel.org/arch/x86/tdx.html)
- [Intel TDX Module Spec (May 2026)](https://www.intel.com/content/www/us/en/content-details/853294/intel-trust-domain-extensions-intel-tdx-module-base-architecture-specification.html)
