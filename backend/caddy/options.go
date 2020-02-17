package caddy

import (
	"encoding/json"
	"time"
)

type Options struct {
	UpstreamTCP    string        `json:"upstream_tcp"`
	UpstreamUDP    string        `json:"upstream_udp"`
	ConnectTimeout time.Duration `json:"connect_timeout"`
	ReadTimeout    time.Duration `json:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout"`
}

func (opts *Options) UnmarshalJSON(data []byte) error {
	var fakeOptions struct {
		UpstreamTCP    string `json:"upstream_tcp"`
		UpstreamUDP    string `json:"upstream_udp"`
		ConnectTimeout string `json:"connect_timeout"`
		ReadTimeout    string `json:"read_timeout"`
		WriteTimeout   string `json:"write_timeout"`
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
	d, err = time.ParseDuration(fakeOptions.ReadTimeout)
	if err != nil {
		return err
	}
	opts.ReadTimeout = d
	d, err = time.ParseDuration(fakeOptions.WriteTimeout)
	if err != nil {
		return err
	}
	opts.WriteTimeout = d
	return nil
}

func (opts *Options) withDefaults() {
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 3 * time.Second
	}
	if opts.ReadTimeout <= 0 {
		opts.ReadTimeout = 1 * time.Minute
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = 5 * time.Second
	}
}
