package main

import "time"

const (
	defaultTimeout       = 30 * time.Second
	defaultCacheTTL      = 30 * time.Second
	defaultCacheMaxMB    = 50
	defaultGroupBy       = "tags"
	defaultProfileSuffix = ".apimount.yaml"
)
