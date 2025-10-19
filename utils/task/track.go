package task

import (
	"main/utils/ampapi"
)

type Track struct {
	ID         string
	Type       string
	Name       string
	Storefront string
	Language   string

	SaveDir    string
	SaveName   string
	SavePath   string
	Codec      string
	TaskNum    int
	TaskTotal  int
	M3u8       string
	WebM3u8    string
	DeviceM3u8 string
	Quality    string
	CoverPath  string

	Resp         ampapi.TrackRespData
	PreType      string // Parent type: album or playlist
	PreID        string // Parent ID
	DiscTotal    int
	AlbumData    ampapi.AlbumRespData
	PlaylistData ampapi.PlaylistRespData
}

func (t *Track) GetAlbumData(token string) error {
	var err error
	resp, err := ampapi.GetAlbumRespByHref(t.Resp.Href, t.Language, token)
	if err != nil {
		return err
	}
	t.AlbumData = resp.Data[0]
	// Try to get the total number of disks in the album where the track is located
	if len(resp.Data) > 0 {
		len := len(resp.Data[0].Relationships.Tracks.Data)
		if len > 0 {
			t.DiscTotal = resp.Data[0].Relationships.Tracks.Data[len-1].Attributes.DiscNumber
		}
	}

	return nil
}
