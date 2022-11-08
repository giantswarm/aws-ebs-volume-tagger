package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Client struct {
	EC2Client *ec2.Client
	K8sClient *kubernetes.Clientset
}

const (
	configNamespace   = "giantswarm"
	configName        = "aws-ebs-volume-tagger-chart-values"
	customerTagPrefix = "tag.provider.giantswarm.io"
)

var kubeConfig = flag.Bool("kubeconfig", false, "out of cluster client configuration")

func newClient(ctx context.Context) *Client {
	flag.Parse()

	awsconfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	imdsClient := imds.NewFromConfig(awsconfig)
	region, err := imdsClient.GetRegion(ctx, &imds.GetRegionInput{})
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	awsconfig.Region = region.Region

	var k8sconfig *rest.Config
	if !(*kubeConfig) {
		k8sconfig, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Unexpected error occured: %v\n", err)
		}
	} else {
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		// use the current context in kubeconfig
		k8sconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("Unexpected error occured: %v\n", err)
		}
	}

	k8sClient, err := kubernetes.NewForConfig(k8sconfig)
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	return &Client{
		EC2Client: ec2.NewFromConfig(awsconfig),
		K8sClient: k8sClient,
	}
}

func main() {
	ctx := context.Background()
	c := newClient(ctx)

	tagsConfigMap, err := c.tagsFromConfigMap(ctx)
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	if len(tagsConfigMap) == 0 {
		log.Print("No tags found in configmap, skipping tagging EBS volumes.")
		os.Exit(0)
	}

	volumeHandles, err := c.listVolumeHandles(ctx)
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	if len(volumeHandles) == 0 {
		log.Print("No persistent volumes in cluster found, skipping tagging EBS volumes.")
		os.Exit(0)
	}

	filteredVolumes, err := c.filteredVolumes(ctx, volumeHandles)
	if err != nil {
		log.Fatalf("Unexpected error occured: %v\n", err)
	}

	if diffTags(tagsConfigMap, filteredVolumes) {
		err = c.deleteTags(ctx, volumeHandles)
		if err != nil {
			log.Fatalf("Unexpected error occured: %v\n", err)
		}

		err = c.createTags(ctx, tagsConfigMap, volumeHandles)
		if err != nil {
			log.Fatalf("Unexpected error occured: %v\n", err)
		}
		log.Print("Updated tags on all EBS volumes.")
		os.Exit(0)
	}
	log.Print("No change needed, no diff between tags from config map and EBS volumes detected.")
	os.Exit(0)
}

func diffTags(cm map[string]string, volumesWithTags []types.Volume) bool {
	count := 0
	for _, volume := range volumesWithTags {
		for _, tag := range volume.Tags {
			if strings.Contains(*tag.Key, customerTagPrefix) {
				v, ok := cm[*tag.Key]
				if !ok {
					return true
				}
				if v != *tag.Value {
					return true
				}
				count++
			}
		}
	}
	return count != len(cm)
}

func (c *Client) listVolumeHandles(ctx context.Context) ([]string, error) {
	list, err := c.K8sClient.CoreV1().PersistentVolumes().List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var volumeHandles []string
	for _, pv := range list.Items {
		if pv.Spec.CSI != nil {
			volumeHandles = append(volumeHandles, pv.Spec.CSI.VolumeHandle)
		}
	}
	return volumeHandles, nil
}

func (c *Client) tagsFromConfigMap(ctx context.Context) (map[string]string, error) {
	cm, err := c.K8sClient.CoreV1().ConfigMaps(configNamespace).Get(ctx, configName, v1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
	}
	var values string
	if content, ok := cm.Data["values"]; ok {
		values = content
	}
	data := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(values), data)
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	if data["extraVolumeTags"] != nil {
		for k, v := range data["extraVolumeTags"].(map[interface{}]interface{}) {
			if strings.Contains(k.(string), customerTagPrefix) {
				tags[k.(string)] = v.(string)
			}
		}
	}

	return tags, nil
}

func (c *Client) createTags(ctx context.Context, cm map[string]string, volumeHandles []string) error {
	var createTagsInput ec2.CreateTagsInput
	createTagsInput.Resources = append(createTagsInput.Resources, volumeHandles...)

	var ebsVolumeTags []types.Tag
	for k, v := range cm {
		ebsVolumeTags = append(ebsVolumeTags, types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	createTagsInput.Tags = ebsVolumeTags
	_, err := c.EC2Client.CreateTags(ctx, &createTagsInput)
	if err != nil {
		return err
	}
	return nil

}

func (c *Client) filteredVolumes(ctx context.Context, volumeHandles []string) ([]types.Volume, error) {
	var filters []types.Filter
	filters = append(filters, types.Filter{Name: aws.String("volume-id"), Values: volumeHandles})

	volumesInput := &ec2.DescribeVolumesInput{Filters: filters}
	output, err := c.EC2Client.DescribeVolumes(ctx, volumesInput)
	if err != nil {
		return nil, err
	}
	return output.Volumes, nil
}

func (c *Client) deleteTags(ctx context.Context, volumeHandles []string) error {
	volumes, err := c.filteredVolumes(ctx, volumeHandles)
	if err != nil {
		return err
	}

	var deleteTags []types.Tag
	for _, volume := range volumes {
		for _, tag := range volume.Tags {
			if strings.Contains(*tag.Key, customerTagPrefix) {
				deleteTags = append(deleteTags, tag)
			}
		}
	}
	if len(deleteTags) != 0 {
		deleteTagsInput := &ec2.DeleteTagsInput{Resources: volumeHandles, Tags: deleteTags}
		_, err = c.EC2Client.DeleteTags(ctx, deleteTagsInput)
		if err != nil {
			return err
		}
	}
	return nil
}
