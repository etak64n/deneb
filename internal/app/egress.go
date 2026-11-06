package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// blockedAddressError says that a webhook would reach an address that webhooks may
// not reach.
type blockedAddressError struct{ addr netip.Addr }

func (e blockedAddressError) Error() string {
	return "the endpoint is at a link-local address, where the metadata services of clouds answer, which webhooks may not reach"
}

// statusError says that an endpoint answered a webhook with a status other than 2xx.
type statusError struct{ code int }

func (e statusError) Error() string {
	return fmt.Sprintf("the endpoint answered %d %s", e.code, http.StatusText(e.code))
}

// metadataServices are the addresses of the metadata services of clouds outside the
// link-local ranges: that of AWS over IPv6, and that of Alibaba Cloud.
var metadataServices = []netip.Addr{netip.MustParseAddr("fd00:ec2::254"), netip.MustParseAddr("100.100.100.200")}

// blockedAddress tells whether webhooks may not reach an address: a link-local one,
// such as 169.254.169.254, where the metadata services of clouds give out credentials,
// or one of metadataServices. A key of the API could otherwise make deneb fetch
// them. Private networks stay open, since the receiver of the webhooks often runs on
// the same host or in the same network as deneb.
func blockedAddress(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if addr.IsLinkLocalUnicast() {
		return true
	}
	for _, service := range metadataServices {
		if addr == service {
			return true
		}
	}
	return false
}

// newWebhookClient returns the client that sends webhooks. It refuses to connect to
// the addresses that blockedAddress names, which it checks after the name of the host
// resolves, so that a DNS answer cannot trick it. It follows no redirects, which would
// turn a POST into a GET without its body.
func newWebhookClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			to, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if blockedAddress(to.Addr()) {
				return blockedAddressError{addr: to.Addr().Unmap()}
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// deliveryError says why a delivery failed, for the deliveries that the API shows to
// any key. It names what deneb saw, not what the endpoint said, so that a delivery
// cannot read what answers at an address. The error itself goes to the log.
func deliveryError(err error) string {
	var blocked blockedAddressError
	var status statusError
	var network net.Error
	switch {
	case errors.As(err, &blocked):
		return blocked.Error()
	case errors.As(err, &status):
		return status.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return "the endpoint did not answer within 15 seconds"
	default:
		return "deneb could not reach the endpoint"
	}
}

// withoutURL drops the URL of the endpoint from an error of the client, since a URL
// may hold a secret, as the URLs of some chat services do.
func withoutURL(err error) error {
	var request *url.Error
	if errors.As(err, &request) {
		return request.Err
	}
	return err
}
