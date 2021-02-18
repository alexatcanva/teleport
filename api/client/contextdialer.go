/*
Copyright 2020 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"net"
	"time"

	"github.com/gravitational/trace"
	"golang.org/x/crypto/ssh"
)

// ContextDialer represents network dialer interface that uses context
type ContextDialer interface {
	// DialContext is a function that dials the specified address
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// ContextDialerFunc is a function wrapper that implements the ContextDialer interface
type ContextDialerFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// DialContext is a function that dials to the specified address
func (f ContextDialerFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return f(ctx, network, addr)
}

// NewAddrDialer makes a new dialer from a list of addresses
func NewAddrDialer(addrs []string, keepAliveInterval, dialTimeout time.Duration) (ContextDialer, error) {
	if len(addrs) == 0 {
		return nil, trace.BadParameter("no addreses to dial")
	}
	dialer := net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: keepAliveInterval,
	}
	return ContextDialerFunc(func(ctx context.Context, network, _ string) (conn net.Conn, err error) {
		for _, addr := range addrs {
			conn, err = dialer.DialContext(ctx, network, addr)
			if err == nil {
				return conn, nil
			}
		}
		// not wrapping on purpose to preserve the original error
		return nil, err
	}), nil
}

func NewProxyDialer(addrs []string, keepAliveInterval, dialTimeout time.Duration, ssh *ssh.ClientConfig) (ContextDialer, error) {
	if len(addrs) == 0 {
		return nil, trace.BadParameter("no addreses to dial")
	}
	if ssh == nil {
		return nil, trace.BadParameter("no ssh config")
	}
	proxyDialer := &TunnelAuthDialer{
		ClientConfig: ssh,
	}
	return ContextDialerFunc(func(ctx context.Context, network, _ string) (conn net.Conn, err error) {
		for _, addr := range addrs {
			proxyDialer.ProxyAddr = addr
			conn, err = proxyDialer.DialContext(ctx, network, addr)
			if err == nil {
				return conn, nil
			}
		}
		return nil, err
	}), nil
}

// NewClientDialer makes a new dialer from a client Config. This dialer
// will try dialing the address as both auth and proxy.
func NewClientDialer(c *Client) (ContextDialer, error) {
	if len(c.c.Addrs) == 0 {
		return nil, trace.BadParameter("no addreses to dial")
	}
	return ContextDialerFunc(func(ctx context.Context, network, _ string) (conn net.Conn, err error) {
		// authDialer := net.Dialer{
		// 	Timeout:   c.DialTimeout,
		// 	KeepAlive: c.KeepAlivePeriod,
		// }
		var errs []error
		for _, addr := range c.c.Addrs {
			// try dialing directly to auth server
			// conn, err = authDialer.DialContext(ctx, network, addr)
			// if err == nil {
			// 	return conn, nil
			// }
			// errs = append(errs, trace.Errorf("failed to dial %v as auth: %v", addr, err))

			// if connecting to auth fails and SSH is defined, try connecting via proxy
			if c.sshConfig == nil {
				continue
			}
			// // Figure out the reverse tunnel address on the proxy first.
			// tunAddr, err := findReverseTunnel(ctx, cfg.AuthServers, clientConfig.TLS.InsecureSkipVerify)
			// if err != nil {
			// 	errs = append(errs, trace.Wrap(err, "failed lookup of proxy reverse tunnel address: %v", err))
			// 	return nil, trace.NewAggregate(errs...)
			// // }
			proxyDialer := &TunnelAuthDialer{
				ProxyAddr:    addr,
				ClientConfig: c.sshConfig,
			}
			conn, err = proxyDialer.DialContext(ctx, network, addr)
			if err == nil {
				return conn, nil
			}
			errs = append(errs, trace.Errorf("failed to dial %v as proxy: %v", addr, err))
		}
		return nil, trace.NewAggregate(errs...)
	}), nil
}
