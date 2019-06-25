package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
	"log"
	"os"
)

func init() {
	exif.RegisterParsers(mknote.All...)
}

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context) error {
	// Load the AWS config
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return err
	}
	// Create a DynamoDB client
	var db = dynamodb.New(cfg)

	// Scan the table
	data, keys, err := scan(ctx, db, nil)
	if err != nil {
		return err
	}

	// Replace the first row with keys
	data = append(data, []string{})
	copy(data[1:], data)
	data[0] = keys

	var body = bytes.NewBuffer([]byte{})
	w := csv.NewWriter(body)

	// Write everything to body var
	if err := w.WriteAll(data); err != nil {
		log.Printf("error writing csv: %v", err)
		return err
	}

	// Create S3 Client
	var s3Client = s3.New(cfg)

	// Put the CSV document into S3 bucket
	_, err = s3Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket:        aws.String(os.Getenv("S3_BUCKET_NAME")),
		Key:           aws.String("image-data.csv"),
		ContentType:   aws.String("text/csv"),
		Body:          bytes.NewReader(body.Bytes()),
		ContentLength: aws.Int64(int64(body.Len())),
		ContentDisposition: aws.String("attachment"),
	}).Send(ctx)
	if err != nil {
		log.Printf("error while uploading CSV to S3: %v", err)
		return err
	}
	log.Println("successfully uploaded CSV to S3")

	return nil
}

func scan(ctx context.Context, db *dynamodb.Client, lek map[string]dynamodb.AttributeValue) ([][]string, []string, error) {
	var data = [][]string{}
	var keysMap = map[string]bool{}

	var scanParams = &dynamodb.ScanInput{
		TableName: aws.String(os.Getenv("DYNAMO_TABLE_IMAGE_DATA")),
	}
	// If we have a LastEvaluatedKey as param, set it as ExclusiveStartKey
	if lek != nil {
		scanParams.ExclusiveStartKey = lek
	}

	// Send the scan request
	sr, err := db.ScanRequest(scanParams).Send(ctx)
	if err != nil {
		log.Printf("error while scanning: %v", err)
		return nil, nil, err
	}

	// If there are no rows throw an error
	if *sr.Count < 1 {
		log.Printf("no records found in scan")
		return nil, nil, errors.New("no records found")
	}

	// Get the unique keys and data
	for _, i := range sr.Items {
		var item = []string{}
		for k, v := range i {
			keysMap[k] = true
			if v.NULL != nil && *v.NULL != false {
				item = append(item, "")
			} else {
				item = append(item, *v.S)
			}
		}
		data = append(data, item)
	}

	// Convert keys map into a slice, so that, it can be used as a row in data
	var keys = []string{}
	for k, _ := range keysMap {
		keys = append(keys, k)
	}

	// Scan requests are capped at 1MB of output data, so, we look for LastEvaluatedKey
	// to determine if there is more data. And use LastEvaluatedKey as ExclusiveStartKey in
	// the next request.
	if len(sr.LastEvaluatedKey) > 0 {
		log.Println("extending the scan request")
		newData, newKeys, err := scan(ctx, db, sr.LastEvaluatedKey)
		if err != nil {
			return nil, nil, err
		}

		// Add the newly fetched data to the existing
		data = append(data, newData...)
		keys = append(keys, newKeys...)
	}

	return data, keys, err
}

func main() {
	lambda.Start(Handler)
}
