package session

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	RegisterDefaults()

	base := Config{
		Mode:      modeSRV,
		Link:      "direct",
		Transport: "datachannel",
		Carrier:   "telemost",
		RoomID:    "room-1",
		ClientID:  "client-1",
		KeyHex:    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		DNSServer: "1.1.1.1:53",
	}

	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "valid baseline", cfg: base},
		{
			name: "jazz allows empty room id",
			cfg: func() Config {
				cfg := base
				cfg.Carrier = "jazz"
				cfg.RoomID = ""
				return cfg
			}(),
		},
		{
			name: "cnc requires socks host and port",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "127.0.0.1"
				cfg.SOCKSPort = 1080
				return cfg
			}(),
		},
		{
			name: "missing mode",
			cfg: func() Config {
				cfg := base
				cfg.Mode = ""
				return cfg
			}(),
			want: ErrModeRequired,
		},
		{
			name: "unsupported carrier",
			cfg: func() Config {
				cfg := base
				cfg.Carrier = "unknown"
				return cfg
			}(),
			want: ErrUnsupportedCarrier,
		},
		{
			name: "unsupported link",
			cfg: func() Config {
				cfg := base
				cfg.Link = "unknown"
				return cfg
			}(),
			want: ErrUnsupportedLink,
		},
		{
			name: "unsupported transport",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "unknown"
				return cfg
			}(),
			want: ErrUnsupportedTransport,
		},
		{
			name: "room id required for non jazz",
			cfg: func() Config {
				cfg := base
				cfg.RoomID = ""
				return cfg
			}(),
			want: ErrRoomIDRequired,
		},
		{
			name: "client id required",
			cfg: func() Config {
				cfg := base
				cfg.ClientID = ""
				return cfg
			}(),
			want: ErrClientIDRequired,
		},
		{
			name: "key required",
			cfg: func() Config {
				cfg := base
				cfg.KeyHex = ""
				return cfg
			}(),
			want: ErrKeyRequired,
		},
		{
			name: "dns server required",
			cfg: func() Config {
				cfg := base
				cfg.DNSServer = ""
				return cfg
			}(),
			want: ErrDNSServerRequired,
		},
		{
			name: "videochannel requires dimensions and bitrate settings",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				return cfg
			}(),
			want: ErrVideoWidthRequired,
		},
		{
			name: "videochannel rejects invalid codec",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = "none"
				cfg.VideoCodec = "bogus"
				return cfg
			}(),
			want: ErrVideoCodecInvalid,
		},
		{
			name: "videochannel requires height",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				return cfg
			}(),
			want: ErrVideoHeightRequired,
		},
		{
			name: "videochannel requires fps",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				return cfg
			}(),
			want: ErrVideoFPSRequired,
		},
		{
			name: "videochannel requires bitrate",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				return cfg
			}(),
			want: ErrVideoBitrateRequired,
		},
		{
			name: "videochannel requires hw",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				return cfg
			}(),
			want: ErrVideoHWRequired,
		},
		{
			name: "tile codec requires square 1080 dimensions",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = "none"
				cfg.VideoCodec = "tile"
				return cfg
			}(),
			want: ErrTileCodecDimensions,
		},
		{
			name: "videochannel valid",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 1080
				cfg.VideoHeight = 1080
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = "none"
				cfg.VideoCodec = "tile"
				return cfg
			}(),
		},
		{
			name: "vp8channel requires fps",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel"
				return cfg
			}(),
			want: ErrVP8FPSRequired,
		},
		{
			name: "vp8channel requires batch size",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel"
				cfg.VP8FPS = 25
				return cfg
			}(),
			want: ErrVP8BatchSizeRequired,
		},
		{
			name: "vp8channel valid",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel"
				cfg.VP8FPS = 25
				cfg.VP8BatchSize = 16
				return cfg
			}(),
		},
		{
			name: "cnc requires socks host",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSPort = 1080
				return cfg
			}(),
			want: ErrSOCKSHostRequired,
		},
		{
			name: "cnc requires socks port",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "127.0.0.1"
				return cfg
			}(),
			want: ErrSOCKSPortRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.cfg)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBuildRoomURL(t *testing.T) {
	tests := []struct {
		carrier string
		roomID  string
		want    string
	}{
		{carrier: "telemost", roomID: "abc", want: "https://telemost.yandex.ru/j/abc"},
		{carrier: "jazz", roomID: "", want: "any"},
		{carrier: "jazz", roomID: "room", want: "room"},
		{carrier: "wbstream", roomID: "wb", want: "wb"},
		{carrier: "other", roomID: "raw", want: "raw"},
	}

	for _, tt := range tests {
		if got := buildRoomURL(tt.carrier, tt.roomID); got != tt.want {
			t.Fatalf("buildRoomURL(%q, %q) = %q, want %q", tt.carrier, tt.roomID, got, tt.want)
		}
	}
}
