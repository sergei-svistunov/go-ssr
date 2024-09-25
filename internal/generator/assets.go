package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Assets struct {
	assets map[string][]string
	images map[string]string
}

func AssetsFromDir(p string) (*Assets, error) {
	f, err := os.Open(filepath.Join(p, "webpack-assets.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Assets{assets: map[string][]string{}}, nil
		}
		return nil, err
	}
	defer f.Close()

	var assetsData struct {
		Entrypoints map[string][]string `json:"entrypoints"`
		Images      map[string]string   `json:"images"`
	}

	if err := json.NewDecoder(f).Decode(&assetsData); err != nil {
		return nil, fmt.Errorf("error decoding webpack-stats.json: %v", err)
	}

	assets := make(map[string][]string)
	for epId, entryPoint := range assetsData.Entrypoints {
		if epId == "main" {
			epId = "/"
		} else {
			epId = strings.TrimPrefix(epId, "pages")
		}

		epAssets := make([]string, len(entryPoint))
		for i, asset := range entryPoint {
			epAssets[i] = asset
		}
		assets[epId] = epAssets
	}

	return &Assets{assets, assetsData.Images}, nil
}

var dqReplacer = strings.NewReplacer(
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
)

func (a *Assets) GetTags(rPath string) []string {
	assets := a.assets[rPath]
	if assets == nil {
		return nil
	}
	result := make([]string, 0, len(assets))
	for _, asset := range assets {
		if strings.HasSuffix(asset, ".css") {
			result = append(result, fmt.Sprintf(`<link href="%s" rel="stylesheet">`, dqReplacer.Replace(asset)))
		} else {
			result = append(result, fmt.Sprintf(`<script defer="defer" src="%s"></script>`, dqReplacer.Replace(asset)))
		}
	}
	return result
}

func (a *Assets) GetImageAsset(image string) string {
	return a.images[image]
}
