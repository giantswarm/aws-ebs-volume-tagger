module github.com/giantswarm/aws-ebs-volume-tagger

go 1.16

require (
	github.com/aws/aws-sdk-go-v2 v1.17.1
	github.com/aws/aws-sdk-go-v2/config v1.2.0
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.1.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.0.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.65.0
	golang.org/x/sys v0.1.0 // indirect
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/apimachinery v0.18.19
	k8s.io/client-go v0.18.19
)

replace github.com/gogo/protobuf v1.3.1 => github.com/gogo/protobuf v1.3.2
