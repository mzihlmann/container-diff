/*
Copyright 2018 Google, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package differs

import (
	pkgutil "github.com/GoogleContainerTools/container-diff/pkg/util"
	"github.com/GoogleContainerTools/container-diff/util"
	"github.com/sirupsen/logrus"
)

type FileMetaAnalyzer struct {
}

func (a FileMetaAnalyzer) Name() string {
	return "FileMetaAnalyzer"
}

// FileDiff diffs two packages and compares their contents
func (a FileMetaAnalyzer) Diff(image1, image2 pkgutil.Image) (util.Result, error) {
	diff, err := diffImageFileMetadata(image1.FSPath, image2.FSPath)
	return &util.MetaDirDiffResult{
		Image1:   image1.Source,
		Image2:   image2.Source,
		DiffType: "FileMeta",
		Diff:     diff,
	}, err
}

func (a FileMetaAnalyzer) Analyze(image pkgutil.Image) (util.Result, error) {
	var result util.FileMetaAnalyzeResult

	imgDir, err := pkgutil.GetDirectory(image.FSPath, true)
	if err != nil {
		return result, err
	}
	entries, err := pkgutil.GetDirectoryMetaEntries(imgDir)
	if err != nil {
		return result, err
	}

	result.Image = image.Source
	result.AnalyzeType = "FileMeta"
	result.Analysis = entries
	return &result, err
}

func diffImageFileMetadata(img1, img2 string) (util.MetaDirDiff, error) {
	var diff util.MetaDirDiff

	img1Dir, err := pkgutil.GetDirectory(img1, true)
	if err != nil {
		return util.MetaDirDiff{}, err
	}
	img2Dir, err := pkgutil.GetDirectory(img2, true)
	if err != nil {
		return util.MetaDirDiff{}, err
	}

	diff, _, err = util.DiffDirectoryMetadata(img1Dir, img2Dir)
	if err != nil {
		return util.MetaDirDiff{}, err
	}

	return diff, nil
}

type FileMetaLayerAnalyzer struct {
}

func (a FileMetaLayerAnalyzer) Name() string {
	return "FileMetaLayerAnalyzer"
}

// FileDiff diffs two packages and compares their contents
func (a FileMetaLayerAnalyzer) Diff(image1, image2 pkgutil.Image) (util.Result, error) {
	var dirDiffs []util.MetaDirDiff

	// Go through each layer of the first image...
	for index, layer := range image1.Layers {
		if index >= len(image2.Layers) {
			continue
		}
		// ...else, diff as usual
		layer2 := image2.Layers[index]
		diff, err := diffImageFileMetadata(layer.FSPath, layer2.FSPath)
		if err != nil {
			return &util.MultipleDirDiffResult{}, err
		}
		dirDiffs = append(dirDiffs, diff)
	}

	// check if there are any additional layers in either image
	if len(image1.Layers) != len(image2.Layers) {
		if len(image1.Layers) > len(image2.Layers) {
			logrus.Infof("%s has additional layers, please use container-diff analyze to view the files in these layers", image1.Source)
		} else {
			logrus.Infof("%s has additional layers, please use container-diff analyze to view the files in these layers", image2.Source)
		}
	}
	return &util.MultipleMetaDirDiffResult{
		Image1:   image1.Source,
		Image2:   image2.Source,
		DiffType: "FileMetaLayer",
		Diff: util.MultipleMetaDirDiff{
			DirDiffs: dirDiffs,
		},
	}, nil
}

func (a FileMetaLayerAnalyzer) Analyze(image pkgutil.Image) (util.Result, error) {
	var directoryEntries [][]pkgutil.DirectoryMetaEntry
	for _, layer := range image.Layers {
		layerDir, err := pkgutil.GetDirectory(layer.FSPath, true)
		if err != nil {
			return util.FileMetaLayerAnalyzeResult{}, err
		}
		entry, err := pkgutil.GetDirectoryMetaEntries(layerDir)
		if err != nil {
			return util.FileMetaLayerAnalyzeResult{}, err
		}
		directoryEntries = append(directoryEntries, entry)
	}

	return &util.FileMetaLayerAnalyzeResult{
		Image:       image.Source,
		AnalyzeType: "FileMetaLayer",
		Analysis:    directoryEntries,
	}, nil
}
