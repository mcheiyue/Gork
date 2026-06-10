// Package backends defines configuration storage backend contracts and
// implementations.
//
// Python app.platform.config.backends package boundary is represented here.
// The package-level API mirrors app/platform/config/backends/__init__.py.
// Python __all__ exports ConfigBackend, create_config_backend, and
// get_config_backend_name; the Go equivalents are ConfigBackend,
// CreateConfigBackend, and GetConfigBackendName.
package backends
