// Package account contains account control-plane models, commands, repository
// contracts, refresh services, and scheduler state.
//
// Package-level exports mirror the public symbols from Python's
// app.control.account package. Repository construction lives in the backends
// subpackage so storage implementations can depend on these contracts without a
// Go import cycle.
package account

func BasicQuotaDefaults() AccountQuotaSet {
	return DefaultQuotaSet("basic")
}

func SuperQuotaDefaults() AccountQuotaSet {
	return DefaultQuotaSet("super")
}
