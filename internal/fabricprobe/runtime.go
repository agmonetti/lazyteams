package fabricprobe

const (
	RejectRuntime         RejectReason = "runtime_error"
	RejectSnapshotChanged RejectReason = "snapshot_changed"
)

// RejectExternal fails the probe without accepting arbitrary error text into evidence.
func (p *Probe) RejectExternal(reason RejectReason) {
	if reason != RejectRuntime && reason != RejectSnapshotChanged {
		reason = RejectRuntime
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reject(reason)
}
