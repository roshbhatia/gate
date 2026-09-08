// Package provider narrows the canonical provider/v1 contract to Gate.
//
// The contract itself is provider.cue in roshbhatia/provider-spec, pinned as
// the provider-spec flake input. This file adds only Gate's rule: every
// provider answers gate.decide. Vet a manifest with both files:
//
//	cue vet -d '#Manifest' "$PROVIDER_SPEC/provider.cue" schema/narrow.cue extras/bash-guard/provider.yaml
package provider

#Manifest: actions: "gate.decide"!: _
