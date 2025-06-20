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

package util

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	pkgutil "github.com/GoogleContainerTools/container-diff/pkg/util"
	"github.com/sirupsen/logrus"

	"github.com/pmezard/go-difflib/difflib"
)

type DirDiff struct {
	Adds []pkgutil.DirectoryEntry
	Dels []pkgutil.DirectoryEntry
	Mods []EntryDiff
}

type MetaDirDiff struct {
	Adds []pkgutil.DirectoryMetaEntry
	Dels []pkgutil.DirectoryMetaEntry
	Mods []MetaEntryDiff
}

type MultipleDirDiff struct {
	DirDiffs []DirDiff
}

type MultipleMetaDirDiff struct {
	DirDiffs []MetaDirDiff
}

type FileNameDiff struct {
	Filename    string
	Description string
	Diff        string
}

type EntryDiff struct {
	Name  string
	Size1 int64
	Size2 int64
}

type MetaEntryDiff struct {
	Name  string
	Mode1 fs.FileMode
	UID1  uint32
	GID1  uint32
	Mode2 fs.FileMode
	UID2  uint32
	GID2  uint32
}

// Modification of difflib's unified differ
func GetAdditions(a, b []string) []string {
	matcher := difflib.NewMatcher(a, b)
	differences := matcher.GetGroupedOpCodes(0)

	adds := []string{}
	for _, group := range differences {
		for _, opCode := range group {
			j1, j2 := opCode.J1, opCode.J2
			if opCode.Tag == 'r' || opCode.Tag == 'i' {
				adds = append(adds, b[j1:j2]...)
			}
		}
	}
	return adds
}

func GetDeletions(a, b []string) []string {
	matcher := difflib.NewMatcher(a, b)
	differences := matcher.GetGroupedOpCodes(0)

	dels := []string{}
	for _, group := range differences {
		for _, opCode := range group {
			i1, i2 := opCode.I1, opCode.I2
			if opCode.Tag == 'r' || opCode.Tag == 'd' {
				dels = append(dels, a[i1:i2]...)
			}
		}
	}
	return dels
}

func GetMatches(a, b []string) []string {
	matcher := difflib.NewMatcher(a, b)
	matchindexes := matcher.GetMatchingBlocks()

	matches := []string{}
	for i, match := range matchindexes {
		if i != len(matchindexes)-1 {
			start := match.A
			end := match.A + match.Size
			matches = append(matches, a[start:end]...)
		}
	}
	return matches
}

// DiffDirectory takes the diff of two directories, assuming both are completely unpacked
func DiffDirectory(d1, d2 pkgutil.Directory) (DirDiff, bool) {
	adds := GetAddedEntries(d1, d2)
	sort.Strings(adds)
	addedEntries := pkgutil.CreateDirectoryEntries(d2.Root, adds)

	dels := GetDeletedEntries(d1, d2)
	sort.Strings(dels)
	deletedEntries := pkgutil.CreateDirectoryEntries(d1.Root, dels)

	mods := GetModifiedEntries(d1, d2)
	sort.Strings(mods)
	modifiedEntries := createEntryDiffs(d1.Root, d2.Root, mods)

	var same bool
	if len(adds) == 0 && len(dels) == 0 && len(mods) == 0 {
		same = true
	} else {
		same = false
	}

	return DirDiff{addedEntries, deletedEntries, modifiedEntries}, same
}

// DiffDirectoryMetadata takes the diff of metadata between two directories, assuming both are completely unpacked
func DiffDirectoryMetadata(d1, d2 pkgutil.Directory) (MetaDirDiff, bool, error) {
	adds := GetAddedEntries(d1, d2)
	sort.Strings(adds)
	addedEntries, err := pkgutil.CreateDirectoryMetaEntries(d2.Root, adds)
	if err != nil {
		return MetaDirDiff{}, false, err
	}

	dels := GetDeletedEntries(d1, d2)
	sort.Strings(dels)
	deletedEntries, err := pkgutil.CreateDirectoryMetaEntries(d1.Root, dels)
	if err != nil {
		return MetaDirDiff{}, false, err
	}

	mods := GetModifiedMetaEntries(d1, d2)
	sort.Strings(mods)
	modifiedEntries, err := createMetaEntryDiffs(d1.Root, d2.Root, mods)
	if err != nil {
		return MetaDirDiff{}, false, err
	}

	var same bool
	if len(adds) == 0 && len(dels) == 0 && len(mods) == 0 {
		same = true
	} else {
		same = false
	}

	return MetaDirDiff{addedEntries, deletedEntries, modifiedEntries}, same, nil
}

func DiffFile(image1, image2 *pkgutil.Image, filename string) (*FileNameDiff, error) {
	//Join paths
	image1FilePath := filepath.Join(image1.FSPath, filename)
	image2FilePath := filepath.Join(image2.FSPath, filename)

	//Get contents of files
	image1FileContents, err := pkgutil.GetFileContents(image1FilePath)
	if err != nil {
		return nil, err
	}

	image2FileContents, err := pkgutil.GetFileContents(image2FilePath)
	if err != nil {
		return nil, err
	}

	description := ""
	//Check if file contents are empty or if they are the same
	if image1FileContents == nil && image2FileContents == nil {
		description := "Both files are empty"
		return &FileNameDiff{filename, description, ""}, nil
	}

	if image1FileContents == nil {
		description := fmt.Sprintf("%s contains an empty file, the contents of %s are:", image1.Source, image2.Source)
		return &FileNameDiff{filename, description, *image2FileContents}, nil
	}

	if image2FileContents == nil {
		description := fmt.Sprintf("%s contains an empty file, the contents of %s are:", image2.Source, image1.Source)
		return &FileNameDiff{filename, description, *image1FileContents}, nil
	}

	if *image1FileContents == *image2FileContents {
		description := "Both files are the same, the contents are:"
		return &FileNameDiff{filename, description, *image1FileContents}, nil
	}

	//Carry on with diffing, make string array for difflib requirements
	image1Contents := difflib.SplitLines(string(*image1FileContents))
	image2Contents := difflib.SplitLines(string(*image2FileContents))

	//Run diff
	diff := difflib.UnifiedDiff{
		A:        image1Contents,
		B:        image2Contents,
		FromFile: image1.Source,
		ToFile:   image2.Source,
	}

	text, err := difflib.GetUnifiedDiffString(diff)

	if err != nil {
		return nil, err
	}
	return &FileNameDiff{filename, description, text}, nil
}

// Checks for metadata differences between files of the same name from different directories
func GetModifiedMetaEntries(d1, d2 pkgutil.Directory) []string {
	d1files := d1.Content
	d2files := d2.Content

	filematches := GetMatches(d1files, d2files)

	modified := []string{}
	for _, f := range filematches {
		f1path := fmt.Sprintf("%s%s", d1.Root, f)
		f2path := fmt.Sprintf("%s%s", d2.Root, f)

		f1stat, err := os.Lstat(f1path)
		if err != nil {
			logrus.Errorf("Error checking directory entry %s: %s\n", f, err)
			continue
		}
		f2stat, err := os.Lstat(f2path)
		if err != nil {
			logrus.Errorf("Error checking directory entry %s: %s\n", f, err)
			continue
		}

		// Compare Mode
		if f1stat.Mode() != f2stat.Mode() {
			modified = append(modified, f)
			continue
		}

		// Compare UID/GID
		s1 := f1stat.Sys().(*syscall.Stat_t)
		s2 := f2stat.Sys().(*syscall.Stat_t)
		if s1.Uid != s2.Uid || s1.Gid != s2.Gid {
			modified = append(modified, f)
			continue
		}
	}
	return modified
}

// Checks for content differences between files of the same name from different directories
func GetModifiedEntries(d1, d2 pkgutil.Directory) []string {
	d1files := d1.Content
	d2files := d2.Content

	filematches := GetMatches(d1files, d2files)

	modified := []string{}
	for _, f := range filematches {
		f1path := fmt.Sprintf("%s%s", d1.Root, f)
		f2path := fmt.Sprintf("%s%s", d2.Root, f)

		f1stat, err := os.Lstat(f1path)
		if err != nil {
			logrus.Errorf("Error checking directory entry %s: %s\n", f, err)
			continue
		}
		f2stat, err := os.Lstat(f2path)
		if err != nil {
			logrus.Errorf("Error checking directory entry %s: %s\n", f, err)
			continue
		}

		// If the directory entry is a symlink, make sure the symlinks point to the same place
		if f1stat.Mode()&os.ModeSymlink != 0 && f2stat.Mode()&os.ModeSymlink != 0 {
			same, err := pkgutil.CheckSameSymlink(f1path, f2path)
			if err != nil {
				logrus.Errorf("Error determining if symlink %s and %s are equivalent: %s\n", f1path, f2path, err)
				continue
			}
			if !same {
				modified = append(modified, f)
			}
			continue
		}

		// If the directory entry in question is a tar, verify that the two have the same size
		if pkgutil.IsTar(f1path) {
			if f1stat.Size() != f2stat.Size() {
				modified = append(modified, f)
			}
			continue
		}

		// If the directory entry is not a tar and not a directory, then it's a file so make sure the file contents are the same
		// Note: We skip over directory entries because to compare directories, we compare their contents
		if !f1stat.IsDir() {
			same, err := pkgutil.CheckSameFile(f1path, f2path)
			if err != nil {
				logrus.Errorf("Error diffing contents of %s and %s: %s\n", f1path, f2path, err)
				continue
			}
			if !same {
				modified = append(modified, f)
			}
		}
	}
	return modified
}

func GetAddedEntries(d1, d2 pkgutil.Directory) []string {
	return GetAdditions(d1.Content, d2.Content)
}

func GetDeletedEntries(d1, d2 pkgutil.Directory) []string {
	return GetDeletions(d1.Content, d2.Content)
}

func createEntryDiffs(root1, root2 string, entryNames []string) (entries []EntryDiff) {
	for _, name := range entryNames {
		entryPath1 := filepath.Join(root1, name)
		size1 := pkgutil.GetSize(entryPath1)

		entryPath2 := filepath.Join(root2, name)
		size2 := pkgutil.GetSize(entryPath2)

		entry := EntryDiff{
			Name:  name,
			Size1: size1,
			Size2: size2,
		}
		entries = append(entries, entry)
	}
	return entries
}

func createMetaEntryDiffs(root1, root2 string, entryNames []string) (entries []MetaEntryDiff, err error) {
	for _, name := range entryNames {
		entryPath1 := filepath.Join(root1, name)
		f1stat, err := os.Lstat(entryPath1)
		if err != nil {
			return nil, err
		}
		s1 := f1stat.Sys().(*syscall.Stat_t)

		entryPath2 := filepath.Join(root2, name)
		f2stat, err := os.Lstat(entryPath2)
		if err != nil {
			return nil, err
		}
		s2 := f2stat.Sys().(*syscall.Stat_t)

		entry := MetaEntryDiff{
			Name:  name,
			Mode1: f1stat.Mode(),
			Mode2: f2stat.Mode(),
			UID1:  s1.Uid,
			UID2:  s2.Uid,
			GID1:  s1.Gid,
			GID2:  s2.Gid,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
