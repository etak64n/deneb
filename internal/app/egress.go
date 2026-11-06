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

// blockedAddressError says that a webhook would reach an address in a private or local
// network that DENEB_WEBHOOK_ALLOW_NETWORKS does not list.
type blockedAddressError struct{ addr netip.Addr }

func (e blockedAddressError) Error() string {
	return "the endpoint is in a private or local network, which DENEB_WEBHOOK_ALLOW_NETWORKS does not list"
}

// statusError says that an endpoint answered a webhook with a status other than 2xx.
type statusError struct{ code int }

func (e statusError) Error() string {
	return fmt.Sprintf("the endpoint answered %d %s", e.code, http.StatusText(e.code))
}

// sharedAddressSpace is 100.64.0.0/10, which carrier-grade NAT and Tailscale use.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// internalAddress tells whether an address is out of reach of the internet: loopback,
// private, link-local, shared, unspecified, "this network", or multicast.
func internalAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() ||
		sharedAddressSpace.Contains(addr) || (addr.Is4() && addr.As4()[0] == 0)
}

// webhookAllowed tells whether webhooks may reach an address: any on the internet, and
// the internal ones in the networks that allow lists.
func webhookAllowed(addr netip.Addr, allow []netip.Prefix) bool {
	addr = addr.Unmap()
	if !internalAddress(addr) {
		return true
	}
	for _, prefix := range allow {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// newWebhookClient returns the client that sends webhooks. It connects only to the
// addresses that webhookAllowed allows, so that a key of the API cannot make deneb
// send requests into the network that deneb runs in. It checks each address after the
// name of the host resolves, which a DNS answer cannot trick. It uses no proxy, whose
// address would be all that it could check, and follows no redirects, which would turn
// a POST into a GET without its body.
func newWebhookClient(allow []netip.Prefix) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			to, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if !webhookAllowed(to.Addr(), allow) {
				return blockedAddressError{addr: to.Addr().Unmap()}
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
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
