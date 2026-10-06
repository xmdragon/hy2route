package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/landing"
	"github.com/xmdragon/hy2route/internal/transport"
	"github.com/xmdragon/hy2route/internal/transport/reality"
)

// probe exercises actual configured egress without starting listeners or
// changing firewall state. Explicit transports are useful before a cutover.
func runProbe(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "/tmp/hy2route/core.json", "configuration path")
	network := flags.String("network", "tcp", "tcp or udp")
	relay := flags.String("transport", "auto", "auto, reality, or hy2")
	url := flags.String("url", "https://api.ipify.org", "TCP probe URL")
	target := flags.String("target", "1.1.1.1:53", "UDP DNS target")
	name := flags.String("name", "example.com", "UDP DNS query name")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if (*network != "tcp" && *network != "udp") || (*relay != "auto" && *relay != "reality" && *relay != "hy2") {
		fmt.Fprintln(stderr, "invalid network or transport")
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	app, err := newApplication(cfg, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer app.hy2Client.Close()
	stream, packet := app.tcp.Proxy, app.udp.Proxy
	if *relay != "auto" {
		var base transport.StreamDialer = app.hy2Client
		packet = app.hy2Client
		if *relay == "reality" {
			if !cfg.TCPRelay.Enabled {
				fmt.Fprintln(stderr, "Reality relay is disabled")
				return 1
			}
			client, e := reality.New(cfg.TCPRelay, cfg.Firewall.BypassMark)
			if e != nil {
				fmt.Fprintln(stderr, e)
				return 1
			}
			base, packet = client, client
		}
		stream, err = landing.New(cfg.Landing, base)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	result := map[string]any{"network": *network, "transport": *relay}
	if *network == "tcp" {
		ht := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) { return stream.Dial(ctx, addr) }}
		defer ht.CloseIdleConnections()
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
		if e != nil {
			err = e
		} else {
			response, e := (&http.Client{Transport: ht}).Do(request)
			if e != nil {
				err = e
			} else {
				defer response.Body.Close()
				var body []byte
				body, err = io.ReadAll(io.LimitReader(response.Body, 4096))
				result["status"] = response.StatusCode
				result["body"] = string(body)
				if response.StatusCode != 200 && response.StatusCode != 204 {
					err = fmt.Errorf("HTTP status %d", response.StatusCode)
				}
			}
		}
	} else {
		var session transport.PacketSession
		session, err = packet.OpenPacket(ctx)
		if err == nil {
			defer session.Close()
			message := new(dns.Msg)
			message.SetQuestion(dns.Fqdn(*name), dns.TypeA)
			wire, _ := message.Pack()
			err = session.Send(wire, *target)
			if err == nil {
				type reply struct {
					payload []byte
					err     error
				}
				ch := make(chan reply, 1)
				go func() { b, _, e := session.Receive(); ch <- reply{b, e} }()
				select {
				case r := <-ch:
					err = r.err
					if err == nil {
						answer := new(dns.Msg)
						err = answer.Unpack(r.payload)
						if err == nil && (answer.Id != message.Id || !answer.Response || answer.Rcode != dns.RcodeSuccess) {
							err = fmt.Errorf("invalid DNS response")
						}
						if err == nil {
							result["answers"] = answer.Answer
						}
					}
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "probe failed: %v\n", err)
		return 1
	}
	result["elapsed_ms"] = time.Since(start).Milliseconds()
	result["tcp_transport"] = app.snapshot().TCPTransport
	result["udp_transport"] = app.snapshot().UDPTransport
	if *relay != "auto" {
		result[*network+"_transport"] = *relay
	}
	if err = json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
