package client

import (
	"badoo/_packages/log"
	lsdProto "github.com/badoo/lsd/proto"
	"regexp"
	"sort"

	"github.com/badoo/lsd/internal/client/network"
	"github.com/badoo/lsd/internal/client/offsets"
	"github.com/badoo/lsd/internal/traffic"
	"errors"
	"strings"
	"sync"
)

func NewNetworkRouter(offsetsDb *offsets.Db, trafficManager *traffic.Manager, config *lsdProto.LsdConfigClientConfigT) (*NetworkRouter, error) {

	// checking that config is correct before doing anything
	hasDefault := false
	for _, confRouting := range config.GetRouting() {

		if len(confRouting.Receivers) == 0 {
			return nil, errors.New("empty receivers section in config")
		}

		if isDefaultSection(confRouting) {
			if hasDefault {
				return nil, errors.New("multiple default sections in config")
			}
			hasDefault = true
		}
	}
	if !hasDefault {
		return nil, errors.New("no default section in config")
	}

	router := &NetworkRouter{
		offsetsDb:      offsetsDb,
		trafficManager: trafficManager,
		balancers:      make(map[string]*network.Balancer),
	}
	// get categories ordered from most specific to least specific, aka
	// 1. categories without wildcards
	// 2. categories with wildcards, sorted by longest literal prefix
	fullMatches := make([]categoryInfo, 0)
	partialMatches := make([]categoryInfo, 0)

	for _, confRouting := range config.GetRouting() {

		if isDefaultSection(confRouting) {
			router.defaultCategory = categoryInfo{
				routingConfig: confRouting,
				outBufferSize: config.GetOutBufferSize(),
			}
			continue
		}

		for _, cat := range confRouting.Categories {

			re := regexp.MustCompile("^" + strings.Replace(regexp.QuoteMeta(cat), "\\*", ".*", -1) + "$")
			prefix, complete := re.LiteralPrefix()

			cat := categoryInfo{
				prefix:        prefix,
				regex:         re,
				outBufferSize: config.GetOutBufferSize(),
				routingConfig: confRouting,
			}
			if complete == true {
				fullMatches = append(fullMatches, cat)
			} else {
				partialMatches = append(partialMatches, cat)
			}
		}
	}

	sort.Slice(partialMatches, func(i, j int) bool {
		return len(partialMatches[i].prefix) > len(partialMatches[j].prefix)
	})

	router.categories = append(fullMatches, partialMatches...)
	return router, nil
}

type NetworkRouter struct {
	trafficManager *traffic.Manager
	offsetsDb      *offsets.Db

	balancers       map[string]*network.Balancer
	categories      []categoryInfo
	defaultCategory categoryInfo
}

func (r *NetworkRouter) stop() {
	wg := sync.WaitGroup{}
	for _, b := range r.balancers {
		wg.Add(1)
		go func(nb *network.Balancer) {
			nb.Stop()
			wg.Done()
		}(b)
	}
	wg.Wait()
}

func (r *NetworkRouter) getOutChanForCategory(category string) chan *lsdProto.RequestNewEventsEventT {

	b, ok := r.balancers[category]
	if ok {
		return b.InChan
	}

	cat := func() categoryInfo {
		for _, cat := range r.categories {
			if cat.regex.MatchString(category) {
				log.Debugf("using %s match for category: %s", cat.regex, category)
				return cat
			}
		}
		log.Debugf("using default route for category: %s", category)
		return r.defaultCategory
	}()
	b = network.NewBalancer(r.offsetsDb, r.trafficManager, cat.outBufferSize, cat.routingConfig)
	b.Start()
	r.balancers[category] = b
	return b.InChan
}

func isDefaultSection(s *lsdProto.LsdConfigClientConfigTRoutingConfigT) bool {
	return len(s.Categories) == 0
}

type categoryInfo struct {
	prefix        string
	regex         *regexp.Regexp
	routingConfig *lsdProto.LsdConfigClientConfigTRoutingConfigT
	outBufferSize uint64
}
