package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
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
	// This marker represents the starting or ending of an XMP tag
	xmpPacketMarker = "<?xpacket"
)

func init() {
	exif.RegisterParsers(mknote.All...)
}

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context, r *events.S3Event) error {
	if r == nil || len(r.Records) < 1 {
		return errors.New("invalid S3 records")
	}

	// Load AWS configuration
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return err
	}

	// Create an S3 client with the config and default options
	var s3Client = s3.New(cfg)
	// Create DynamoDB client
	var db = dynamodb.New(cfg)

	// We're not going to abort on error but log them to CloudWatch
	for _, item := range r.Records {
		// Get the Object specified in current Record
		gor, err := s3Client.GetObjectRequest(&s3.GetObjectInput{
			Bucket: aws.String(item.S3.Bucket.Name),
			Key: aws.String(item.S3.Object.Key),
		}).Send(ctx)
		if err != nil {
			log.Printf("error while S3 getting object: %v", err)
			continue
		}
		// We only want to process images
		if !strings.HasPrefix(*gor.ContentType, "image/") {
			log.Printf("expected file type encountered while iterating over event records: %s", *gor.ContentType)
			continue
		}

		var data = map[string]dynamodb.AttributeValue{}

		// Read the body because we want to analyse it
		body, err := ioutil.ReadAll(gor.Body)
		if err != nil {
			log.Printf("error while reading S3 object body: %v", err)
			continue
		}
		if err = gor.Body.Close(); err != nil {
			log.Printf("error while closing S3 GetObjectRequest body")
		}

		// Run the body through EXIF decoder
		x, err := exif.Decode(bytes.NewReader(body))
		if err != nil {
			log.Printf("error while decoding EXIF data: %v", err)
			continue
		}
		var w = exifWalker{fields: map[string]interface{}{}}
		// Walk the EXIF data, so that, we can set them as `fields` in variable w
		if err = x.Walk(w); err != nil {
			log.Printf("error while walking EXIF fields: %v", err)
			continue
		}
		for key, val := range w.fields {
			switch v := val.(type) {
			// We're saving everything as string at the moment
			case string:
				// Store as null if value is empty
				if len(v) < 1 {
					data[key] = dynamodb.AttributeValue{NULL: aws.Bool(true)}
				} else {
					data[key] = dynamodb.AttributeValue{S: aws.String(v)}
				}
			default:
				log.Printf("unexpected type found while ranging EXIF fields: %v", v)
				continue
			}
		}

		// We want to look for two XMP tags (opening and closing)
		if bytes.Count(body, []byte(xmpPacketMarker)) != 2 {
			log.Printf("error while finding XMP document: %v", err)
			continue
		}
		// Get the index, so that, we can use it to split the data and get the XMP document
		var xmpIndex = bytes.Index(body, []byte(xmpPacketMarker))

		var d = xmp.NewDocument()
		d.SetDirty()
		// Use the XMP index to split the data, and get XMP document
		if err := xmp.Unmarshal(body[xmpIndex:], d); err != nil {
			log.Printf("error while parsing XMP document: %v", err)
			continue
		}
		// Get the XMP paths so that can we iterate over them
		paths, err := d.ListPaths()
		if err != nil {
			log.Printf("error while listing XMP paths: %v", err)
			continue
		}
		for _, p := range paths {
			// Set the attribute as null if the value is empty
			if len(p.Value) < 1 {
				data[string(p.Path)] = dynamodb.AttributeValue{NULL: aws.Bool(true)}
			} else {
				data[string(p.Path)] = dynamodb.AttributeValue{S: aws.String(p.Value)}
			}
		}

		// Manually set the etag and key attributes after removing the quotes from values
		data["etag"] = dynamodb.AttributeValue{S: aws.String(strings.TrimRight(strings.TrimLeft(*gor.ETag, `"`), `"`))}
		data["key"] = dynamodb.AttributeValue{S: aws.String(strings.TrimRight(strings.TrimLeft(item.S3.Object.Key, `"`), `"`))}

		// Put the item to DynamoDB table
		_, err = db.PutItemRequest(&dynamodb.PutItemInput{
			TableName: aws.String(os.Getenv("DYNAMO_TABLE_IMAGE_DATA")),
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
		log.Printf("nil tag encountered in EXIF: %v", name)
		return nil
	}
	var value string
	switch tag.Id {
	// TODO: Fix XP tag representation
	// XP tags are not being represented correctly for some reason
	case 0x9c9e, 0x9c9f, 0x9c9d, 0x9c9c, 0x9c9b:
		// TODO: Find a better way to represent XP tags
		value = tag.String()
	default:
		value = tag.String()
	}

	// Trim the quotes
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = strings.TrimRight(strings.TrimLeft(value, `"`), `"`)
	}

	w.fields[string(name)] = value

	return nil
}

func main() {
	lambda.Start(Handler)
}
