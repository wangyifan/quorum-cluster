package main

import (
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/urfave/cli/v2"

	"fmt"
	"log"
)

var regions = []string{"us-east-1", "us-west-1"}

const instancePrefix = "Quorum-cluster"
const quorumClusterPath = "/tmp/quorum-cluster"

func createSingleInstance(svc *ec2.EC2, instanceConfig map[string]string, sgConfig []*string, instanceName string) error {
	// Specify the details of the instance that you want to create.
	runResult, err := svc.RunInstances(&ec2.RunInstancesInput{
		ImageId:          aws.String(instanceConfig["ami"]),
		InstanceType:     aws.String("t2.micro"),
		MinCount:         aws.Int64(1),
		MaxCount:         aws.Int64(1),
		KeyName:          aws.String(instanceConfig["key"]),
		SecurityGroupIds: sgConfig,
	})

	if err != nil {
		fmt.Printf("Could not create instance: %s err: %s\n", instanceName, err)
		return err
	}

	fmt.Printf("Created instance %s: %s\n", instanceName, *runResult.Instances[0].InstanceId)

	// Add tags to the created instance
	_, errtag := svc.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{runResult.Instances[0].InstanceId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(instanceName),
			},
		},
	})
	if errtag != nil {
		log.Println("Could not create tags for instance", runResult.Instances[0].InstanceId, errtag)
		return errtag
	}

	fmt.Println("Successfully tagged instance")
	return nil
}

func createRegionInstances(
	region string,
	regionConfig map[string]string,
	sgConfig []*string,
	regionInstanceCount int,
	name string) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	if err != nil {
		log.Fatal("Can not create aws ec2 session")
	}
	svc := ec2.New(sess)

	for i := 0; i < regionInstanceCount; i++ {
		instanceName := fmt.Sprintf("%s-%s-%s-T%2d", instancePrefix, name, region, i+1)
		createSingleInstance(svc, regionConfig, sgConfig, instanceName)
	}
}

func createFullCluster(totalInstanceCount int, name string) error {
	regionInstances := getRegionInstanceCount(totalInstanceCount, regions)
	for i := 0; i < len(regions); i++ {
		region := regions[i]
		regionConfig, sgConfig := getRegionConfig(region)
		createRegionInstances(region, regionConfig, sgConfig, regionInstances[region], name)
	}
	return nil
}

func getRegionConfig(region string) (map[string]string, []*string) {
	eastAMI := "ami-05389a60399ebe46e"
	eastKey := "Tradove Check | ED25519"
	eastSgPing := "sg-03c8000a6a670ee66"
	eastSgQuorum := "sg-02a7c0b3d9b7d647e"
	eastSgSSH := "sg-0fa2bb781e968ff1d"
	eastSgBlockscout := "sg-007ff1d1aaa989776"
	eastSgConfig := []*string{&eastSgPing, &eastSgQuorum, &eastSgSSH, &eastSgBlockscout}

	westAMI := "ami-0647199721088964c"
	westKey := "Tradove | ED25519 | dev"
	westSgPing := "sg-07409fa2af10a693b"
	westSgQuorum := "sg-00226ca88cb8a403b"
	westSgSSH := "sg-0cd62803f0920b1ac"
	westSgBlockscout := "sg-09f35cf807118c573"
	westSgConfig := []*string{&westSgPing, &westSgQuorum, &westSgSSH, &westSgBlockscout}

	regionConfigEast := map[string]string{
		"ami": eastAMI,
		"key": eastKey,
	}
	regionConfigWest := map[string]string{
		"ami": westAMI,
		"key": westKey,
	}

	if strings.Contains(region, "west") {
		return regionConfigWest, westSgConfig
	} else if strings.Contains(region, "east") {
		return regionConfigEast, eastSgConfig
	} else {
		log.Fatal(fmt.Sprintf("unknown region: %s", region))
	}

	return nil, nil
}

func getRegionInstanceCount(totalInstanceCount int, regions []string) map[string]int {
	regionInstanceCount := make(map[string]int)
	for i := 0; i < totalInstanceCount; i++ {
		index := i % len(regions)
		regionInstanceCount[regions[index]] += 1
	}

	return regionInstanceCount
}

func listInstances(stop bool) []*ec2.Instance {
	instances := make([]*ec2.Instance, 0, 50)
	for i := 0; i < len(regions); i++ {
		region := regions[i]
		sess, err := session.NewSession(&aws.Config{
			Region: aws.String(region)},
		)
		if err != nil {
			log.Fatal("Can not create aws ec2 session")
		}
		svc := ec2.New(sess)

		filters := []*ec2.Filter{
			{
				Name:   aws.String("instance-state-name"),
				Values: []*string{aws.String("running"), aws.String("pending")},
			},
		}

		request := ec2.DescribeInstancesInput{Filters: filters}
		svc.DescribeInstancesPages(&request,
			func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
				for i := 0; i < len(page.Reservations); i++ {
					for j := 0; j < len(page.Reservations[i].Instances); j++ {
						instance := page.Reservations[i].Instances[j]
						instanceName := *instance.Tags[0].Value
						if !stop {
							if strings.Contains(instanceName, instancePrefix) {
								instances = append(instances, instance)
							}
						} else {
							// kill instances whose name has prefix
							if strings.Contains(instanceName, instancePrefix) {
								instances = append(instances, instance)
								input := &ec2.StopInstancesInput{
									InstanceIds: []*string{
										aws.String(*instance.InstanceId),
									},
								}

								result, err := svc.StopInstances(input)
								if err != nil {
									fmt.Println(err)
								} else {
									fmt.Println(result)
								}
							}
						}
					}
				}
				return !lastPage
			})
	}

	return instances
}

func printInstances(instances []*ec2.Instance) {
	for _, instance := range instances {
		fmt.Printf(
			"Instance: %s, %s, %s, %s\n",
			*instance.InstanceId, *instance.State.Name, *instance.Tags[0].Value, *instance.PublicIpAddress,
		)
	}
}

func waitForInstances(t time.Duration) {
	fmt.Print("\033[s")
	targetTime := time.Now().Add(t)
	for time.Now().Before(targetTime) {
		fmt.Print("\033[u\033[K")
		diff := time.Until(targetTime)
		fmt.Printf("Wait for EC2 instances to be ready...%d", int(diff.Seconds()))
		time.Sleep(time.Second)
	}
	fmt.Println()
}

func actionHandler(c *cli.Context) error {
	if c.Bool("list") {
		instances := listInstances(false)
		printInstances(instances)
	} else if c.Bool("stop") {
		listInstances(true)
	} else if c.Bool("start") {
		totalInstanceCount := c.Int("count")
		if !c.Bool("current-ec2") {
			name := c.String("name")
			createFullCluster(totalInstanceCount, name)
			waitForInstances(3 * time.Minute)
		}
		resetDir(quorumClusterPath)
		generateQuorumConfig(c.Int("count"))
		executeBlockchain(c.Int("count"))
	} else if c.Bool("reset") {
		resetDir(quorumClusterPath)
	} else {
		fmt.Println("No action")
	}

	return nil
}

func main() {
	app := &cli.App{
		Name:  "Quorum Cluster",
		Usage: "Create a quorum cluster",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "count",
				Value: 1,
				Usage: "Total number of instances in the cluster",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "Name of the cluster",
			},
			&cli.BoolFlag{
				Name:  "start",
				Usage: "Start new quorum cluster",
			},
			&cli.BoolFlag{
				Name:  "list",
				Usage: "List running or pending instances",
			},
			&cli.BoolFlag{
				Name:  "stop",
				Usage: "Stop running or pending instances of Quorum cluster",
			},
			&cli.BoolFlag{
				Name:  "current-ec2",
				Usage: "Run the cluster on existing ec2 machines",
			},
			&cli.BoolFlag{
				Name:  "reset",
				Usage: "Reset quorum cluster config directory",
			},
		},
		Action: actionHandler,
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
