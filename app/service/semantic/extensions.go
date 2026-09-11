package semantic

import (
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/serrors"
)

// WithExtensionCapabilities attaches explicit extension Interpreters and the
// Renderer-aware capability registry used by Compile, Validate, and Explain.
func (s *CompileService) WithExtensionCapabilities(inventory *extension.Inventory, capabilities *extension.Registry) *CompileService {
	if s == nil {
		return s
	}
	if inventory != nil {
		s.extensionInventory = inventory
	}
	if capabilities != nil {
		s.extensionCapabilities = capabilities
	}
	if s.resolver != nil {
		s.resolver.WithExtensionCapabilities(s.extensionInventory, s.extensionCapabilities)
	}
	return s
}

func (s *CompileService) validateExtensionCapabilities(model *ossie.SemanticModel, renderer renderer.Renderer) error {
	if s == nil || s.extensionInventory == nil {
		return nil
	}
	requirements, err := s.extensionInventory.RequirementsForModel(model)
	if err != nil {
		return err
	}
	if renderer == nil {
		return &serrors.Error{Code: serrors.ErrTargetRequired, Message: "selected Renderer is required for extension capability validation"}
	}
	selected := extension.RendererContext{Dialect: string(renderer.SQLDialect())}
	for _, requirement := range requirements {
		resolution := s.extensionCapabilities.Resolve(requirement, selected)
		if resolution.State == extension.StateUnsupportedCritical {
			return newUnsupportedSemanticExtensionError(&extension.UnsupportedError{Resolution: resolution})
		}
	}
	return nil
}

type unsupportedSemanticExtensionError struct {
	semantic    *serrors.Error
	unsupported *extension.UnsupportedError
}

func newUnsupportedSemanticExtensionError(unsupported *extension.UnsupportedError) error {
	if unsupported == nil {
		return nil
	}
	resolution := unsupported.Resolution
	return &unsupportedSemanticExtensionError{
		semantic: &serrors.Error{
			Code:    serrors.ErrUnsupportedSemanticExtension,
			Message: "semantic-critical extension is unsupported by the selected Renderer",
			Details: map[string]any{
				"extension_identity": resolution.Identity.String(),
				"capability":         resolution.Capability,
				"version":            resolution.Version,
				"dialect":            resolution.Renderer.Dialect,
				"reason":             string(resolution.Reason),
			},
		},
		unsupported: unsupported,
	}
}

func (e *unsupportedSemanticExtensionError) Error() string {
	if e == nil || e.semantic == nil {
		return ""
	}
	return e.semantic.Error()
}

func (e *unsupportedSemanticExtensionError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return []error{e.semantic, e.unsupported}
}
