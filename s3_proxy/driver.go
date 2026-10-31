package s3_proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	stdpath "path"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type S3Proxy struct {
	model.Storage
	Addition
	Session   *session.Session
	client    *s3.S3
	transport *http.Transport

	config driver.Config
}

func (d *S3Proxy) Config() driver.Config {
	return d.config
}

func (d *S3Proxy) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *S3Proxy) Init(ctx context.Context) error {
	if d.Region == "" {
		d.Region = "openlist"
	}
	if err := d.initSession(); err != nil {
		return err
	}
	d.client = d.getClient()
	return nil
}

func (d *S3Proxy) Drop(ctx context.Context) error {
	if d.transport != nil {
		d.transport.CloseIdleConnections()
	}
	return nil
}

func (d *S3Proxy) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.ListObjectVersion == "v2" {
		return d.listV2(dir.GetPath(), args)
	}
	return d.listV1(dir.GetPath(), args)
}

// Link returns a RangeReader rather than a presigned URL. The reader performs
// GetObject with the SOCKS5-backed HTTP client, so download traffic never
// connects to the S3 endpoint from the host network.
func (d *S3Proxy) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	key := getKey(file.GetPath(), false)
	disposition := ""
	if d.AddFilenameToDisposition {
		disposition = utils.GenerateContentDisposition(file.GetName())
	}
	return &model.Link{
		RangeReader: stream.RangeReaderFunc(func(ctx context.Context, requested http_range.Range) (io.ReadCloser, error) {
			return d.getObjectRange(ctx, key, requested, disposition)
		}),
		ContentLength: file.GetSize(),
	}, nil
}

func (d *S3Proxy) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return d.Put(ctx, &model.Object{
		Path: stdpath.Join(parentDir.GetPath(), dirName),
	}, &stream.FileStream{
		Obj: &model.Object{
			Name:     getPlaceholderName(d.Placeholder),
			Modified: time.Now(),
		},
		Reader:   bytes.NewReader([]byte{}),
		Mimetype: "application/octet-stream",
	}, func(float64) {})
}

func (d *S3Proxy) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	err := d.Copy(ctx, srcObj, dstDir)
	if err != nil {
		return err
	}
	return d.Remove(ctx, srcObj)
}

func (d *S3Proxy) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	err := d.copy(ctx, srcObj.GetPath(), stdpath.Join(stdpath.Dir(srcObj.GetPath()), newName), srcObj.GetSize(), srcObj.IsDir())
	if err != nil {
		return err
	}
	return d.Remove(ctx, srcObj)
}

func (d *S3Proxy) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return d.copy(ctx, srcObj.GetPath(), stdpath.Join(dstDir.GetPath(), srcObj.GetName()), srcObj.GetSize(), srcObj.IsDir())
}

func (d *S3Proxy) Remove(ctx context.Context, obj model.Obj) error {
	if obj.IsDir() {
		return d.removeDir(ctx, obj.GetPath())
	}
	return d.removeFile(ctx, obj.GetPath())
}

func (d *S3Proxy) Put(ctx context.Context, dstDir model.Obj, s model.FileStreamer, up driver.UpdateProgress) error {
	uploader := s3manager.NewUploader(d.Session)
	if s.GetSize() > s3manager.MaxUploadParts*s3manager.DefaultUploadPartSize {
		uploader.PartSize = s.GetSize() / (s3manager.MaxUploadParts - 1)
	}
	key := getKey(stdpath.Join(dstDir.GetPath(), s.GetName()), false)
	contentType := s.GetMimetype()
	log.Debugln("key:", key)
	input := &s3manager.UploadInput{
		Bucket: &d.Bucket,
		Key:    &key,
		Body: driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
			Reader:         s,
			UpdateProgress: up,
		}),
		ContentType: &contentType,
	}
	_, err := uploader.UploadWithContext(ctx, input)
	return err
}

// implements driver.Getter interface
func (d *S3Proxy) Get(ctx context.Context, path string) (model.Obj, error) {
	// try to get object as a file using HeadObject
	rootPath := d.GetRootPath()
	// Avoid double-prepending root path when path already contains it.
	// This happens when obj.GetPath() from a previous Get call is passed back
	// to op.List → op.Get → d.Get.
	if rootPath != "/" && utils.IsSubPath(rootPath, path) {
		// path already includes the root prefix
	} else {
		path = stdpath.Join(rootPath, path)
	}
	key := getKey(path, false)
	headInput := &s3.HeadObjectInput{
		Bucket: &d.Bucket,
		Key:    &key,
	}
	headOutput, err := d.client.HeadObjectWithContext(ctx, headInput)
	if err == nil {
		// Object exists as a file
		fileName := stdpath.Base(path)
		return &model.Object{
			Name:     fileName,
			Size:     *headOutput.ContentLength,
			Modified: *headOutput.LastModified,
			Path:     path,
		}, nil
	}
	var awsErr awserr.Error
	if errors.As(err, &awsErr) && awsErr.Code() != "NotFound" {
		return nil, errors.WithMessage(err, "failed to head object")
	}

	// If HeadObject fails with 404, check if it's a directory
	prefix := getKey(path, true)
	var contents []*s3.Object
	var commonPrefixes []*s3.CommonPrefix
	switch d.ListObjectVersion {
	case "v1":
		listInput := &s3.ListObjectsInput{
			Bucket:  &d.Bucket,
			Prefix:  &prefix,
			MaxKeys: aws.Int64(1), // Only need to check if at least one object exists
		}
		listResult, err := d.client.ListObjectsWithContext(ctx, listInput)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to list objects with prefix")
		}
		contents = listResult.Contents
		commonPrefixes = listResult.CommonPrefixes
	case "v2":
		listInput := &s3.ListObjectsV2Input{
			Bucket:  &d.Bucket,
			Prefix:  &prefix,
			MaxKeys: aws.Int64(1),
		}
		listResult, err := d.client.ListObjectsV2WithContext(ctx, listInput)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to list objects v2 with prefix")
		}
		contents = listResult.Contents
		commonPrefixes = listResult.CommonPrefixes
	default:
		return nil, fmt.Errorf("unsupported ListObjectVersion: %s", d.ListObjectVersion)
	}
	if len(contents) > 0 || len(commonPrefixes) > 0 {
		dirName := stdpath.Base(path)
		return &model.Object{
			Name:     dirName,
			Modified: d.Modified,
			IsFolder: true,
			Path:     path,
		}, nil
	}
	return nil, errs.ObjectNotFound
}

var _ driver.Driver = (*S3Proxy)(nil)
var _ driver.Getter = (*S3Proxy)(nil)
