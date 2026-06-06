package onedrive

import "time"

type host struct {
	oauth string
	api   string
}

var hostMap = map[string]host{
	"global": {
		oauth: "https://login.microsoftonline.com",
		api:   "https://graph.microsoft.com",
	},
	"cn": {
		oauth: "https://login.chinacloudapi.cn",
		api:   "https://microsoftgraph.chinacloudapi.cn",
	},
	"us": {
		oauth: "https://login.microsoftonline.us",
		api:   "https://graph.microsoft.us",
	},
	"de": {
		oauth: "https://login.microsoftonline.de",
		api:   "https://graph.microsoft.de",
	},
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
	Text         string `json:"text"`
}

type graphErrorResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type graphItem struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Size           int64           `json:"size"`
	FileSystemInfo *fileSystemInfo `json:"fileSystemInfo"`
	DownloadURL    string          `json:"@microsoft.graph.downloadUrl"`
	File           *fileFacet      `json:"file"`
	Folder         *folderFacet    `json:"folder"`
	Thumbnails     []thumbnail     `json:"thumbnails"`
	ParentRef      parentRef       `json:"parentReference"`
}

type fileSystemInfo struct {
	CreatedDateTime      time.Time `json:"createdDateTime"`
	LastModifiedDateTime time.Time `json:"lastModifiedDateTime"`
}

type fileFacet struct {
	MimeType string `json:"mimeType"`
}

type folderFacet struct {
	ChildCount int `json:"childCount"`
}

type thumbnail struct {
	Medium struct {
		URL string `json:"url"`
	} `json:"medium"`
}

type parentRef struct {
	ID      string `json:"id"`
	DriveID string `json:"driveId"`
}

type filesResp struct {
	Value    []graphItem `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

type UploadResult struct {
	FileID string
	Hash   string
	Size   int64
}

type uploadSessionResp struct {
	UploadURL string `json:"uploadUrl"`
}
