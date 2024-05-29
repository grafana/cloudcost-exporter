# Services

This package contains a subset of AWS services that are being used from AWS SDK v2.
The services should be interfaces that define the methods that are being used from the AWS SDK v2.
We do this so that we can generate mocks for these services and use them in our tests.
For example, see:
- [mocks](../../mocks/aws/services)
- [tests](../../pkg/aws/services/s3/s3_test.go)

## Generated Mocks

The mocks for these services are generated using [mockery]()
To generate mocks for these services, run the following command:

```bash
make generate-mocks
```
