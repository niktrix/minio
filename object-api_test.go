/*
 * Minio Cloud Storage, (C) 2015, 2016 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
)

type MySuite struct{}

var _ = Suite(&MySuite{})

func (s *MySuite) TestFSAPISuite(c *C) {
	var storageList []string

	// Initialize name space lock.
	initNSLock()

	create := func() ObjectLayer {
		path, err := ioutil.TempDir(os.TempDir(), "minio-")
		c.Check(err, IsNil)
		objAPI, err := newFSObjects(path)
		c.Check(err, IsNil)
		storageList = append(storageList, path)
		return objAPI
	}
	APITestSuite(c, create)
	defer removeRootsC(c, storageList)
}

func (s *MySuite) TestXLAPISuite(c *C) {
	var storageList []string

	// Initialize name space lock.
	initNSLock()

	create := func() ObjectLayer {
		var nDisks = 16 // Maximum disks.
		var erasureDisks []string
		for i := 0; i < nDisks; i++ {
			path, err := ioutil.TempDir(os.TempDir(), "minio-")
			c.Check(err, IsNil)
			erasureDisks = append(erasureDisks, path)
		}
		objAPI, err := newXLObjects(erasureDisks)
		c.Check(err, IsNil)
		return objAPI
	}
	APITestSuite(c, create)
	defer removeRootsC(c, storageList)
}

func removeRootsC(c *C, roots []string) {
	for _, root := range roots {
		removeAll(root)
	}
}
