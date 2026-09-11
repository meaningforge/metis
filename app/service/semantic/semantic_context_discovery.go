package semantic

// NewSemanticContextServiceFromDiscovery keeps transport wiring on the same
// immutable SemanticManifest store already used by DiscoveryService.
func NewSemanticContextServiceFromDiscovery(discovery *DiscoveryService) *SemanticContextService {
	if discovery == nil {
		return nil
	}
	return &SemanticContextService{manifest: discovery.manifest, discovery: discovery}
}
