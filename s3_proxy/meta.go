package s3_proxy

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootPath
	Bucket                   string `json:"bucket" required:"true"`
	Endpoint                 string `json:"endpoint" required:"true"`
	Region                   string `json:"region"`
	AccessKeyID              string `json:"access_key_id" required:"true"`
	SecretAccessKey          string `json:"secret_access_key" required:"true"`
	SessionToken             string `json:"session_token"`
	Placeholder              string `json:"placeholder"`
	ForcePathStyle           bool   `json:"force_path_style"`
	ListObjectVersion        string `json:"list_object_version" type:"select" options:"v1,v2" default:"v1"`
	AddFilenameToDisposition bool   `json:"add_filename_to_disposition" help:"Add filename to Content-Disposition header."`
	UserAgent                string `json:"user_agent" required:"false" default:"" help:"Custom User-Agent for S3 requests."`
	ProxyURL                 string `json:"proxy_url" required:"true" help:"SOCKS5 proxy URL used for all S3 requests, e.g. socks5://127.0.0.1:1080 or socks5h://user:pass@127.0.0.1:1080."`
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &S3Proxy{
			config: driver.Config{
				Name:        "S3 Proxy",
				DefaultRoot: "/",
				LocalSort:   true,
				CheckStatus: true,
				OnlyProxy:   true,
				NoLinkURL:   true,
			},
		}
	})
}
