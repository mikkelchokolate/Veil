package olcrtc

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks creates client links for an olcRTC inbound.
func (Plugin) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	auth := olcrtcAuth(settings, inbound)
	transport := olcrtcTransport(settings, inbound)
	roomID := olcrtcRoomID(settings, inbound)
	key := olcrtcKey(inbound)
	creds, err := clientaccess.BuildClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		link := model.ClientLink{
			Name:      inbound.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.OlcrtcClientURI(auth, transport, roomID, key, ""),
		}
		return []model.ClientLink{link}, nil
	}
	links := make([]model.ClientLink, 0, len(creds))
	for _, cred := range creds {
		link := model.ClientLink{
			Name:      inbound.Name + "/" + cred.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.OlcrtcClientURI(auth, transport, roomID, key, cred.Username),
		}
		links = append(links, link)
	}
	return links, nil
}
