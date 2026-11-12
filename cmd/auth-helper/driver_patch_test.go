package main

import (
	"strings"
	"testing"
)

func TestPatchDriverBundleGuardsUnknownFrameNavigation(t *testing.T) {
	const driver = `      frameCommittedNewDocumentNavigation(frameId, url2, name, documentId, initial) {
        const frame = this._frames.get(frameId);
        this.removeChildFramesRecursively(frame);
        this._clearWebSockets(frame);
        frame._url = url2;`
	patched := patchDriverBundle(driver)
	if !strings.Contains(patched, "const frame = this._frames.get(frameId);\n        if (!frame)\n          return;\n        this.removeChildFramesRecursively(frame);") {
		t.Fatalf("unknown frame is still dereferenced:\n%s", patched)
	}
	if again := patchDriverBundle(patched); again != patched {
		t.Fatal("patch is not idempotent")
	}
}
