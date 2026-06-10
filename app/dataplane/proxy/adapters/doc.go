// Package adapters contains proxy request adapters for headers, profiles, and
// resettable HTTP sessions used by dataplane transports.
//
// The package-level API mirrors Python's app.dataplane.proxy.adapters package
// boundary by exposing BuildHTTPHeaders, BuildSSOCookie, BuildWSHeaders,
// ResettableSession, BuildSessionKwargs, and NormalizeProxyURL.
package adapters
