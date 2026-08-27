package cast

import (
	"fmt"
	"net"
	"strings"

	"mpdtui/internal/mpdclient"
)

// httpdConfigHint is shown when MPD has no httpd output -- the one thing
// casting can't work around.
const httpdConfigHint = `casting needs an httpd audio output in MPD. Add to mpd.conf:

    audio_output {
        type    "httpd"
        name    "mpdtui cast"
        port    "8000"
        format  "44100:16:2"
        encoder "lame"
    }

then restart MPD.`

// deriveStreamURL works out the URL a cast device should pull audio from.
// Resolution order for the port: an explicit cast_stream_url override
// wins entirely; otherwise the httpd output's own reported port, then
// httpd_port from config, then 8000. The host is cast_stream_host if set,
// else the address mpdtui dialed MPD on -- which must not be a loopback
// address, since a LAN device can't reach that.
func deriveStreamURL(cfg Config, httpd mpdclient.Output) (string, error) {
	if cfg.StreamURL != "" {
		return cfg.StreamURL + "/", nil
	}

	host := cfg.StreamHost
	if host == "" {
		host = hostOnly(cfg.MPDHost)
	}
	if host == "" || isLoopback(host) {
		if lan := primaryLANIP(); lan != "" {
			host = lan
		} else {
			return "", fmt.Errorf("MPD is on %q, which a cast device can't reach -- set cast_stream_host in ~/.config/mpdtui/config to this machine's LAN IP", cfg.MPDHost)
		}
	}

	port := "8000"
	if p, ok := httpd.Attrs["port"]; ok && p != "" {
		port = p
	} else if cfg.HTTPDPort != "" {
		port = cfg.HTTPDPort
	}

	return fmt.Sprintf("http://%s/", net.JoinHostPort(host, port)), nil
}

func hostOnly(addr string) string {
	if addr == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// primaryLANIP returns this machine's first non-loopback IPv4 address, or
// "" if it can't be determined.
func primaryLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

// mediaMetaFromSong builds the now-playing metadata handed to a device.
func mediaMetaFromSong(s mpdclient.Song) MediaMeta {
	title := s.Title
	if title == "" {
		title = baseName(s.File)
	}
	return MediaMeta{Title: title, Artist: s.Artist}
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
