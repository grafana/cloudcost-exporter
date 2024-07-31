# AWS EC2 Instances Multiple Profiles

Used to do a proof of concept for [173](https://github.com/grafana/cloudcost-exporter/issues/173).
The primary goal is to write a simple program that 
1. accepts many different role arn's
2. instantiates an ec2 client _per role_
3. collects ec2 instances by client and region

## Usage

```
go run . -role-arn [...]
```
