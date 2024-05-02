# AWS

This module is responsible for collecting and exporting costs associated with AWS resources.
`aws.go` is the entrypoint for the module and is responsible for setting up the AWS session and starting the collection process.
The module is built upon the aws-sdk-go library and uses the Cost Explorer API to collect cost data.

