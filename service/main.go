package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
	"github.com/rwcarlsen/goexif/tiff"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"trimmer.io/go-xmp/xmp"
)

const (
	xmpPacketMarker = "<?xpacket"
	dynamoTable = "image-data"
)

func init() {
	exif.RegisterParsers(mknote.All...)
}

// Response is of type APIGatewayProxyResponse since we're leveraging the
// AWS Lambda Proxy Request functionality (default behavior)
//
// https://serverless.com/framework/docs/providers/aws/events/apigateway/#lambda-proxy-integration
type Response events.APIGatewayProxyResponse

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context, r *events.S3Event) error {
	if r == nil || len(r.Records) < 1 {
		return errors.New("invalid S3 records")
	}

	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return err
	}
	cfg.Region = endpoints.UsWest2RegionID

	// Create an S3 client with the config and default options
	var s3Client = s3.New(cfg)

	var dbCfg = cfg.Copy()
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "test" {
		dbCfg.EndpointResolver = aws.ResolveWithEndpointURL("http://host.docker.internal:8000")
	}
	var db = dynamodb.New(dbCfg)

	for _, item := range r.Records {
		gor, err := s3Client.GetObjectRequest(&s3.GetObjectInput{
			Bucket: aws.String(item.S3.Bucket.Name),
			Key: aws.String(item.S3.Object.Key),
		}).Send(ctx)
		if err != nil {
			log.Printf("error while S3 getting object: %v", err)
			continue
		}
		if !strings.HasPrefix(*gor.ContentType, "image/") {
			log.Printf("expected file type encountered while iterating over event records: %s", *gor.ContentType)
			continue
		}

		var data = map[string]dynamodb.AttributeValue{}

		body, err := ioutil.ReadAll(gor.Body)
		if err != nil {
			log.Printf("error while reading S3 object body: %v", err)
			continue
		}
		gor.Body.Close()

		x, err := exif.Decode(bytes.NewReader(body))
		if err != nil {
			log.Printf("error while decoding EXIF data: %v", err)
			continue
		}
		var w = exifWalker{fields: map[string]interface{}{}}
		if err = x.Walk(w); err != nil {
			log.Printf("error while walking EXIF fields: %v", err)
			continue
		}
		for key, val := range w.fields {
			switch v := val.(type) {
			case string:
				data[key] = dynamodb.AttributeValue{S: aws.String(v)}
			default:
				log.Printf("unexpected type found while ranging EXIF fields: %v", v)
				continue
			}
		}

		if bytes.Count(body, []byte(xmpPacketMarker)) != 2 {
			log.Printf("error while finding XMP document: %v", err)
			continue
		}
		var xmpIndex = bytes.Index(body, []byte(xmpPacketMarker))
		var d = xmp.NewDocument()
		d.SetDirty()
		if err := xmp.Unmarshal(body[xmpIndex:], d); err != nil {
			log.Printf("error while parsing XMP document: %v", err)
			continue
		}
		paths, err := d.ListPaths()
		if err != nil {
			log.Printf("error while listing XMP paths: %v", err)
			continue
		}
		for _, p := range paths {
			data[string(p.Path)] = dynamodb.AttributeValue{S: aws.String(p.Value)}
		}
		data["etag"] = dynamodb.AttributeValue{S: gor.ETag}

		_, err = db.PutItemRequest(&dynamodb.PutItemInput{
			TableName: aws.String(dynamoTable),
			Item: data,
		}).Send(ctx)
		if err != nil {
			log.Printf("error while puting an item into DynamoDB: %v", err)
			continue
		}
	}

	return nil
}

type exifWalker struct {
	fields map[string]interface{}
}
func (w exifWalker) Walk (name exif.FieldName, tag *tiff.Tag) error {
	if tag == nil {
		return nil
	}
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
