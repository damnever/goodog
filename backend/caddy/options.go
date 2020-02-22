package caddy

import (
	"encoding/json"
	"time"
)

type Options struct {
	UpstreamTCP    string        `json:"upstream_tcp"`
	UpstreamUDP    string        `json:"upstream_udp"`
	ConnectTimeout time.Duration `json:"connect_timeout"`
	Timeout        time.Duration `json:"timeout"`
}

func (opts *Options) UnmarshalJSON(data []byte) error {
	var fakeOptions struct {
		UpstreamTCP    string `json:"upstream_tcp"`
		UpstreamUDP    string `json:"upstream_udp"`
		ConnectTimeout string `json:"connect_timeout"`
		Timeout        string `json:"timeout"`
	}
	if err := json.Unmarshal(data, &fakeOptions); err != nil {
		return err
	}

	opts.UpstreamTCP = fakeOptions.UpstreamTCP
	opts.UpstreamUDP = fakeOptions.UpstreamUDP
	// FUCK????
	d, err := time.ParseDuration(fakeOptions.ConnectTimeout)
	if err != nil {
		return err
	}
	opts.ConnectTimeout = d
	d, err = time.ParseDuration(fakeOptions.Timeout)
	if err != nil {
		return err
	}
	opts.Timeout = d
	return nil
}

func (opts *Options) withDefaults() {
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 3 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 1 * time.Minute
	}
}
