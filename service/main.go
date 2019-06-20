package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
	"github.com/rwcarlsen/goexif/tiff"
	"io/ioutil"
	"log"
	"strings"
	"trimmer.io/go-xmp/xmp"
)

var (
	awsBucket = aws.String("skycatch-engineering-challenges")
	awsMarker = aws.String("201905-platform-extract-xmp-metadata/photos/")
	internalServerError = errors.New("internal server error")
)

const xmpPacketMarker = "<?xpacket"

func init() {
	exif.RegisterParsers(mknote.All...)
}

// Response is of type APIGatewayProxyResponse since we're leveraging the
// AWS Lambda Proxy Request functionality (default behavior)
//
// https://serverless.com/framework/docs/providers/aws/events/apigateway/#lambda-proxy-integration
type Response events.APIGatewayProxyResponse

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context, r *events.APIGatewayProxyRequest) (Response, error) {
	var res = Response{
		StatusCode: 500,
		Headers: map[string]string{},
		IsBase64Encoded: false,
	}

	// The config the S3 Uploader will use
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return res, err
	}
	cfg.Region = endpoints.UsWest2RegionID

	// Create an uploader with the config and default options
	s3Client := s3.New(cfg)

	lor, err := s3Client.ListObjectsRequest(&s3.ListObjectsInput{
		Bucket: awsBucket,
		Marker: awsMarker,
	}).Send(ctx)
	if err != nil {
		return res, err
	}

	var data = map[string]map[string]interface{}{}

	for _, item := range lor.Contents {
		if item.Key == nil {
			continue
		}
		gor, err := s3Client.GetObjectRequest(&s3.GetObjectInput{
			Bucket: awsBucket,
			Key: item.Key,
		}).Send(ctx)
		if err != nil {
			return res, err
		}
		if gor.ContentType == nil {
			return res, internalServerError
		}
		if !strings.HasPrefix(*gor.ContentType, "image/") {
			continue
		}

		body, err := ioutil.ReadAll(gor.Body)
		if err != nil {
			return res, err
		}
		gor.Body.Close()

		x, err := exif.Decode(bytes.NewReader(body))
		if err != nil {
			return res, err
		}
		var w = walker{fields: map[string]interface{}{}}
		if err = x.Walk(w); err != nil {
			return res, err
		}

		data[*item.Key] = w.fields

		if bytes.Count(body, []byte(xmpPacketMarker)) != 2 {
			return res, internalServerError
		}
		var xmpIndex = bytes.Index(body, []byte(xmpPacketMarker))

		var d = xmp.NewDocument()
		d.SetDirty()
		if err := xmp.Unmarshal(body[xmpIndex:], d); err != nil {
			return res, err
		}
		paths, err := d.ListPaths()
		if err != nil {
			return res, err
		}

		for _, path := range paths {
			data[*item.Key][path.Path.String()] = path.Value
		}

		break
	}

	d, err := json.Marshal(data)
	if err != nil {
		return res, err
	}

	res.Body = string(d)

	return res, nil
}

type walker struct {
	fields map[string]interface{}
}
func (w walker) Walk (name exif.FieldName, tag *tiff.Tag) error {
	if tag == nil {
		return nil
	}
	log.Println(tag.Format())
	switch tag.Id {
	// XP tags are not being represented correctly for some reason
	case 0x9c9e, 0x9c9f, 0x9c9d, 0x9c9c, 0x9c9b:
		// TODO: Find a better way to represent XP tags
		w.fields[string(name)] = tag.String()
	default:
		w.fields[string(name)] = tag.String()
	}

	return nil
}

func main() {
	lambda.Start(Handler)
}
