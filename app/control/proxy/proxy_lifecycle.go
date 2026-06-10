package proxy

import "context"

type refreshEvent struct {
	done chan struct{}
}

func (d *ProxyDirectory) InvalidateClearance(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, bundle := range d.bundles {
		bundle.State = ClearanceBundleInvalid
		d.bundles[key] = bundle
	}
	return nil
}

func (d *ProxyDirectory) WarmUp(ctx context.Context) error {
	mode, nodes := d.lifecycleSnapshot()
	if mode == ClearanceModeNone {
		return nil
	}
	for _, item := range affinityItems(nodes) {
		if _, err := d.getOrBuildBundle(ctx, item.affinity, item.proxyURL, defaultClearanceOrigin); err != nil {
			return err
		}
	}
	return nil
}

func (d *ProxyDirectory) RefreshClearanceSafe(ctx context.Context) error {
	mode, nodes, existing := d.refreshSnapshot()
	if mode == ClearanceModeNone {
		return nil
	}
	targets := map[BundleKey]refreshTarget{}
	for _, item := range affinityItems(nodes) {
		key := BundleKey{Affinity: item.affinity, ClearanceHost: clearanceHost(defaultClearanceOrigin)}
		targets[key] = refreshTarget{proxyURL: item.proxyURL, origin: defaultClearanceOrigin}
	}
	for _, key := range existing {
		if _, ok := targets[key]; !ok {
			proxyURL := key.Affinity
			if proxyURL == "direct" {
				proxyURL = ""
			}
			targets[key] = refreshTarget{proxyURL: proxyURL, origin: "https://" + key.ClearanceHost}
		}
	}
	for key, target := range targets {
		bundle, ok, err := d.refreshBundle(ctx, mode, key.Affinity, key.ClearanceHost, target)
		if err != nil {
			return err
		}
		if ok {
			d.mu.Lock()
			d.bundles[key] = bundle
			d.mu.Unlock()
		}
	}
	return nil
}

func (d *ProxyDirectory) getOrBuildBundle(ctx context.Context, affinityKey, proxyURL, origin string) (*ClearanceBundle, error) {
	host := clearanceHost(origin)
	key := BundleKey{Affinity: affinityKey, ClearanceHost: host}
	for {
		mode, event, winner, bundle := d.bundleRefreshState(key)
		if mode == ClearanceModeNone {
			return nil, nil
		}
		if bundle != nil {
			return bundle, nil
		}
		if winner {
			return d.buildBundleAsWinner(ctx, mode, key, proxyURL, origin, event)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-event.done:
		}
	}
}

func (d *ProxyDirectory) bundleRefreshState(key BundleKey) (ClearanceMode, *refreshEvent, bool, *ClearanceBundle) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.clearanceMode == ClearanceModeNone {
		return d.clearanceMode, nil, false, nil
	}
	if bundle, ok := d.bundles[key]; ok && bundle.State == ClearanceBundleValid {
		return d.clearanceMode, nil, false, &bundle
	}
	if event, ok := d.refreshEvents[key]; ok {
		return d.clearanceMode, event, false, nil
	}
	event := &refreshEvent{done: make(chan struct{})}
	d.refreshEvents[key] = event
	return d.clearanceMode, event, true, nil
}

func (d *ProxyDirectory) buildBundleAsWinner(ctx context.Context, mode ClearanceMode, key BundleKey, proxyURL, origin string, event *refreshEvent) (*ClearanceBundle, error) {
	defer d.finishRefresh(key, event)
	bundle, ok, err := d.refreshBundle(ctx, mode, key.Affinity, key.ClearanceHost, refreshTarget{proxyURL: proxyURL, origin: origin})
	if err != nil || !ok {
		return nil, err
	}
	d.mu.Lock()
	d.bundles[key] = bundle
	d.mu.Unlock()
	return &bundle, nil
}

func (d *ProxyDirectory) refreshBundle(ctx context.Context, mode ClearanceMode, affinity, host string, target refreshTarget) (ClearanceBundle, bool, error) {
	var bundle ClearanceBundle
	var ok bool
	var err error
	if mode == ClearanceModeManual {
		bundle, ok, err = d.manual.BuildBundle(affinity, host)
	} else {
		bundle, ok, err = d.flare.RefreshBundle(ctx, affinity, target.proxyURL, target.origin)
	}
	if err != nil || !ok {
		return bundle, ok, err
	}
	if bundle.AffinityKey == "" {
		bundle.AffinityKey = affinity
	}
	if bundle.ClearanceHost == "" {
		bundle.ClearanceHost = host
	}
	return bundle, true, nil
}

func (d *ProxyDirectory) finishRefresh(key BundleKey, event *refreshEvent) {
	d.mu.Lock()
	delete(d.refreshEvents, key)
	d.mu.Unlock()
	close(event.done)
}

func (d *ProxyDirectory) lifecycleSnapshot() (ClearanceMode, []EgressNode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.clearanceMode, copyNodes(d.nodes)
}

func (d *ProxyDirectory) refreshSnapshot() (ClearanceMode, []EgressNode, []BundleKey) {
	d.mu.Lock()
	defer d.mu.Unlock()
	keys := make([]BundleKey, 0, len(d.bundles))
	for key := range d.bundles {
		keys = append(keys, key)
	}
	return d.clearanceMode, copyNodes(d.nodes), keys
}
