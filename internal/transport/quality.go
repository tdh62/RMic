package transport

import (
	"fmt"

	"cnb.cool/tdh6/RMic/internal/audio"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// Each peer negotiates its own Opus parameters. Keep the generated SDP intact
// so Pion's local description and the answer delivered to the browser agree.
func opusCodec(quality audio.Quality) webrtc.RTPCodecParameters {
	stereo := 0
	if quality.Channels == 2 {
		stereo = 1
	}
	return webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: audio.SampleRate, Channels: audio.Channels,
		SDPFmtpLine: fmt.Sprintf("minptime=10;useinbandfec=0;stereo=%d;sprop-stereo=%d;maxaveragebitrate=%d;maxplaybackrate=48000", stereo, stereo, quality.Bitrate),
	}, PayloadType: 111}
}
func (s *Server) newPeerConnection(quality audio.Quality) (*webrtc.PeerConnection, error) {
	media := &webrtc.MediaEngine{}
	err := media.RegisterCodec(opusCodec(quality), webrtc.RTPCodecTypeAudio)
	if err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err = webrtc.RegisterDefaultInterceptors(media, registry); err != nil {
		return nil, err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithSettingEngine(s.settings), webrtc.WithInterceptorRegistry(registry))
	return api.NewPeerConnection(webrtc.Configuration{})
}
