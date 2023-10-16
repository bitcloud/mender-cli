// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package deployments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/areader"

	"github.com/mendersoftware/mender-cli/client"
	"github.com/mendersoftware/mender-cli/log"
)

const (
	httpErrorBoundary = 300
)

type artifactsList struct {
	artifacts []artifactData
}

type artifactData struct {
	ID                    string   `json:"id"`
	Description           string   `json:"description"`
	Name                  string   `json:"name"`
	DeviceTypesCompatible []string `json:"device_types_compatible"`
	Info                  struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	} `json:"info"`
	Signed  bool `json:"signed"`
	Updates []struct {
		TypeInfo struct {
			Type string `json:"type"`
		} `json:"type_info"`
		Files []struct {
			Name     string    `json:"name"`
			Checksum string    `json:"checksum"`
			Size     int       `json:"size"`
			Date     time.Time `json:"date"`
		} `json:"files"`
		MetaData []interface{} `json:"meta_data"`
	} `json:"updates"`
	ArtifactProvides struct {
		ArtifactName string `json:"artifact_name"`
	} `json:"artifact_provides"`
	ArtifactDepends struct {
		DeviceType []string `json:"device_type"`
	} `json:"artifact_depends"`
	Size     int       `json:"size"`
	Modified time.Time `json:"modified"`
}

const (
	artifactUploadURL   = "/api/management/v1/deployments/artifacts"
	artifactsListURL    = artifactUploadURL
	artifactsDeleteURL  = artifactUploadURL
	directUploadURL     = "/api/management/v1/deployments/artifacts/directupload"
	transferCompleteURL = "/api/management/v1/deployments/artifacts/directupload/:id/complete"
)

type Client struct {
	url               string
	artifactUploadURL string
	artifactsListURL  string
	artifactDeleteURL string
	directUploadURL   string
	client            *http.Client
}

type Link struct {
	Uri    string            `json:"uri"`
	Header map[string]string `json:"header,omitempty"`
}

type UploadLink struct {
	ArtifactID string `json:"id"`

	Link
}

func NewClient(url string, skipVerify bool) *Client {
	return &Client{
		url:               url,
		artifactUploadURL: client.JoinURL(url, artifactUploadURL),
		artifactsListURL:  client.JoinURL(url, artifactsListURL),
		artifactDeleteURL: client.JoinURL(url, artifactsDeleteURL),
		directUploadURL:   client.JoinURL(url, directUploadURL),
		client:            client.NewHttpClient(skipVerify),
	}
}

func (c *Client) DirectDownloadLink(token string) (*UploadLink, error) {
	var link UploadLink

	body, err := client.DoPostRequest(token, c.directUploadURL, c.client, nil)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &link)
	if err != nil {
		return nil, err
	}

	return &link, nil
}

func (c *Client) ListArtifacts(token string, detailLevel int) error {
	if detailLevel > 3 || detailLevel < 0 {
		return fmt.Errorf("FAILURE: invalid artifact detail")
	}

	body, err := client.DoGetRequest(token, c.artifactsListURL, c.client)
	if err != nil {
		return err
	}

	var list artifactsList
	err = json.Unmarshal(body, &list.artifacts)
	if err != nil {
		return err
	}
	for _, v := range list.artifacts {
		listArtifact(v, detailLevel)
	}

	return nil
}

func listArtifact(a artifactData, detailLevel int) {
	fmt.Printf("ID: %s\n", a.ID)
	fmt.Printf("Name: %s\n", a.Name)
	if detailLevel >= 1 {
		fmt.Printf("Signed: %t\n", a.Signed)
		fmt.Printf("Modfied: %s\n", a.Modified)
		fmt.Printf("Size: %d\n", a.Size)
		fmt.Printf("Description: %s\n", a.Description)
		fmt.Println("Compatible device types:")
		for _, v := range a.DeviceTypesCompatible {
			fmt.Printf("  %s\n", v)
		}
		fmt.Printf("Artifact format: %s\n", a.Info.Format)
		fmt.Printf("Format version: %d\n", a.Info.Version)
	}
	if detailLevel >= 2 {
		fmt.Printf("Artifact provides: %s\n", a.ArtifactProvides.ArtifactName)
		fmt.Println("Artifact depends:")
		for _, v := range a.ArtifactDepends.DeviceType {
			fmt.Printf("  %s\n", v)
		}
		fmt.Println("Updates:")
		for _, v := range a.Updates {
			fmt.Printf("  Type: %s\n", v.TypeInfo.Type)
			fmt.Println("  Files:")
			for _, f := range v.Files {
				fmt.Printf("\tName: %s\n", f.Name)
				fmt.Printf("\tChecksum: %s\n", f.Checksum)
				fmt.Printf("\tSize: %d\n", f.Size)
				fmt.Printf("\tDate: %s\n", f.Date)
				if len(v.Files) > 1 {
					fmt.Println()
				}
			}
			if detailLevel == 3 {
				fmt.Printf("  MetaData: %v\n", v.MetaData)
			}
		}
	}

	fmt.Println("--------------------------------------------------------------------------------")
}

// Type info structure
type ArtifactUpdateTypeInfo struct {
	Type *string `json:"type" valid:"required"`
}

// Update file structure
type UpdateFile struct {
	// Image name
	Name string `json:"name" valid:"required"`

	// Image file checksum
	Checksum string `json:"checksum" valid:"optional"`

	// Image size
	Size int64 `json:"size" valid:"optional"`

	// Date build
	Date *time.Time `json:"date" valid:"optional"`
}

// Update structure
type Update struct {
	TypeInfo ArtifactUpdateTypeInfo `json:"type_info" valid:"required"`
	Files    []UpdateFile           `json:"files"`
	MetaData interface{}            `json:"meta_data,omitempty" valid:"optional"`
}

type DirectUploadMetadata struct {
	Size    int64    `json:"size,omitempty" valid:"-"`
	Updates []Update `json:"updates" valid:"-"`
}

func readArtifactMetadata(path string, size int64) io.Reader {
	log.Verbf("reading artifact file...")
	r, err := os.Open(path)
	if err != nil {
		log.Verbf("failed to open artifact file: %s", err.Error())
		return nil
	}
	defer r.Close()
	ar := areader.NewReader(r)
	err = ar.ReadArtifact()
	if err != nil {
		log.Verbf("failed to read artifact file: %s", err.Error())
		return nil
	}
	handlers := ar.GetHandlers()
	directUploads := make([]Update, len(handlers))
	for i, p := range handlers {
		files := p.GetUpdateAllFiles()
		if len(files) < 1 {
			log.Verbf("the artifact has no files information")
			return nil
		}
		for _, f := range files {
			directUploads[i].Files = append(directUploads[i].Files, UpdateFile{
				Name:     f.Name,
				Checksum: string(f.Checksum),
				Size:     f.Size,
				Date:     &f.Date,
			})
		}
	}
	directMetadata := DirectUploadMetadata{
		Size:    size,
		Updates: directUploads,
	}
	data, err := json.Marshal(directMetadata)
	if err != nil {
		log.Verbf("failed to parse artifact metadata: %s", err.Error())
		return nil
	}
	log.Verbf("done reading artifact file.")
	return bytes.NewBuffer(data)
}

func (c *Client) DirectUpload(
	token, artifactPath, id, url string,
	headers map[string]string,
	noProgress bool,
) error {
	var bar *pb.ProgressBar

	artifact, err := os.Open(artifactPath)
	if err != nil {
		return errors.Wrap(err, "Cannot read artifact file")
	}
	defer artifact.Close()

	artifactStats, err := artifact.Stat()
	if err != nil {
		return errors.Wrap(err, "Cannot read artifact file stats")
	}

	var req *http.Request
	if !noProgress {
		// create progress bar
		bar = pb.New64(artifactStats.Size()).
			Set(pb.Bytes, true).
			SetRefreshRate(time.Millisecond * 100)
		bar.Start()
		req, err = http.NewRequest(http.MethodPut, url, bar.NewProxyReader(artifact))
	} else {
		req, err = http.NewRequest(http.MethodPut, url, artifact)
	}
	if err != nil {
		return errors.Wrap(err, "Cannot create request")
	}
	req.Header.Set("Content-Type", "application/vnd.mender-artifact")
	req.ContentLength = artifactStats.Size()

	reqDump, _ := httputil.DumpRequest(req, false)
	log.Verbf("sending request: \n%v", string(reqDump))

	for k, h := range headers {
		req.Header.Set(k, h)
	}
	rsp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "POST /artifacts request failed")
	}
	defer rsp.Body.Close()

	rspDump, _ := httputil.DumpResponse(rsp, true)
	log.Verbf("response: \n%v\n", string(rspDump))

	if rsp.StatusCode >= httpErrorBoundary {
		return errors.New(
			fmt.Sprintf("artifact upload to '%s' failed with status %d", req.Host, rsp.StatusCode),
		)
	} else {
		body := readArtifactMetadata(artifactPath, artifactStats.Size())
		_, err := client.DoPostRequest(
			token,
			client.JoinURL(
				c.url,
				strings.ReplaceAll(transferCompleteURL, ":id", id),
			),
			c.client,
			body,
		)
		if err != nil {
			return errors.Wrap(err, "failed to notify on complete upload")
		}
	}

	return nil
}

func (c *Client) UploadArtifact(
	description, artifactPath, token string,
	noProgress bool,
) error {
	var bar *pb.ProgressBar

	artifact, err := os.Open(artifactPath)
	if err != nil {
		return errors.Wrap(err, "Cannot read artifact file")
	}

	artifactStats, err := artifact.Stat()
	if err != nil {
		return errors.Wrap(err, "Cannot read artifact file stats")
	}

	// create pipe
	pR, pW := io.Pipe()

	// create multipart writer
	writer := multipart.NewWriter(pW)

	req, err := http.NewRequest(http.MethodPost, c.artifactUploadURL, pR)
	if err != nil {
		return errors.Wrap(err, "Cannot create request")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+string(token))

	reqDump, _ := httputil.DumpRequest(req, false)
	log.Verbf("sending request: \n%v", string(reqDump))

	if !noProgress {
		// create progress bar
		bar = pb.New64(artifactStats.Size()).
			Set(pb.Bytes, true).
			SetRefreshRate(time.Millisecond * 100)
		bar.Start()
	}

	go func() {
		var part io.Writer
		defer pW.Close()
		defer artifact.Close()

		_ = writer.WriteField("size", strconv.FormatInt(artifactStats.Size(), 10))
		_ = writer.WriteField("description", description)
		part, _ = writer.CreateFormFile("artifact", artifactStats.Name())

		if !noProgress {
			part = bar.NewProxyWriter(part)
		}

		if _, err := io.Copy(part, artifact); err != nil {
			writer.Close()
			_ = pR.CloseWithError(err)
			return
		}

		writer.Close()
		if !noProgress {
			log.Info("Processing uploaded file. This may take around one minute.\n")
		}
	}()

	rsp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "POST /artifacts request failed")
	}
	defer rsp.Body.Close()
	pR.Close()

	rspDump, _ := httputil.DumpResponse(rsp, true)
	log.Verbf("response: \n%v\n", string(rspDump))

	if rsp.StatusCode != http.StatusCreated {
		if rsp.StatusCode == http.StatusUnauthorized {
			log.Verbf("artifact upload to '%s' failed with status %d", req.Host, rsp.StatusCode)
			return errors.New("Unauthorized. Please Login first")
		}
		return errors.New(
			fmt.Sprintf("artifact upload to '%s' failed with status %d", req.Host, rsp.StatusCode),
		)
	}

	return nil
}

func (c *Client) DeleteArtifact(
	artifactID, token string,
) error {

	req, err := http.NewRequest(http.MethodDelete, c.artifactDeleteURL+"/"+artifactID, nil)
	if err != nil {
		return errors.Wrap(err, "Cannot create request")
	}
	req.Header.Set("Authorization", "Bearer "+string(token))

	reqDump, _ := httputil.DumpRequest(req, false)
	log.Verbf("sending request: \n%v", string(reqDump))

	rsp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "DELETE /artifacts request failed")
	}
	defer rsp.Body.Close()

	rspDump, _ := httputil.DumpResponse(rsp, true)
	log.Verbf("response: \n%v\n", string(rspDump))

	if rsp.StatusCode != http.StatusNoContent {
		body, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return errors.Wrap(err, "can't read request body")
		}
		if rsp.StatusCode == http.StatusUnauthorized {
			log.Verbf("artifact delete failed with status %d, reason: %s", rsp.StatusCode, body)
			return errors.New("Unauthorized. Please Login first")
		}
		return errors.New(
			fmt.Sprintf("artifact upload failed with status %d, reason: %s", rsp.StatusCode, body),
		)
	}

	return nil
}
