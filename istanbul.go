package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
)

const chainID = 10

func useKey() string {
	key := os.Getenv("QUORUM_CLUSTER_ED25519")
	if len(key) == 0 {
		log.Fatal("No quorum cluster ed25519 set")
	}

	return key
}

func resetDir(path string) {
	if err := os.RemoveAll(path); err != nil {
		log.Fatal(err)
	}

	if err := os.Mkdir(path, os.ModePerm); err != nil {
		log.Fatal(err)
	}
}

func updateStaticNodesConfig(count int, instances []*ec2.Instance) {
	staticNodesFile := filepath.Join(quorumClusterPath, "static-nodes.json")
	staticNodes := make([]string, 0, 50)
	content, _ := ioutil.ReadFile(staticNodesFile)
	_ = json.Unmarshal([]byte(content), &staticNodes)
	if len(instances) != len(staticNodes) {
		log.Fatal(
			"Static nodes replacement mismatch", len(instances), len(staticNodes),
		)
	}
	newStaticNodes := make([]string, len(staticNodes))
	fmt.Println("Static nodes:")
	for i := 0; i < len(staticNodes); i++ {
		newStaticNodes[i] = strings.Replace(staticNodes[i], "0.0.0.0", *instances[i].PublicIpAddress, 1)
		fmt.Println(newStaticNodes[i])
	}
	newStaticNodesBytes, _ := json.Marshal(newStaticNodes)
	ioutil.WriteFile(staticNodesFile, newStaticNodesBytes, 0644)
}

func uploadConfigFilesAndInit(instances []*ec2.Instance) {
	for index, instance := range instances {
		//ssh -i ~/.ssh/TradoveCheckED25519US.pem -o "StrictHostKeyChecking=no" ubuntu@54.173.147.146 'rm -rf /home/ubuntu/data;mkdir /home/ubuntu/data'
		// clear the data directory on remote machine
		app := "ssh"
		args := []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			fmt.Sprintf("ubuntu@%s", *instance.PublicIpAddress),
			"rm -rf /home/ubuntu/data;mkdir /home/ubuntu/data",
		}
		cmd := exec.Command(app, args...)
		_, err := cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] Data directory reset\n", *instance.PublicIpAddress)
		}

		//scp -i ~/.ssh/TradoveCheckED25519US.pem -o "StrictHostKeyChecking=no" /tmp/quorum-cluster/static-nodes.json ubuntu@54.173.147.146:/home/ubuntu/data
		// copy static nodes file into data directory
		app = "scp"
		args = []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			filepath.Join(quorumClusterPath, "static-nodes.json"),
			fmt.Sprintf("ubuntu@%s:/home/ubuntu/data", *instance.PublicIpAddress),
		}
		cmd = exec.Command(app, args...)
		_, err = cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] Static-nodes.json uploaded\n", *instance.PublicIpAddress)
		}

		//scp -i ~/.ssh/TradoveCheckED25519US.pem -o "StrictHostKeyChecking=no" /tmp/quorum-cluster/genesis.json ubuntu@54.173.147.146:/home/ubuntu/data
		// copy genesis file into data directory
		app = "scp"
		args = []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			filepath.Join(quorumClusterPath, "genesis.json"),
			fmt.Sprintf("ubuntu@%s:/home/ubuntu/data", *instance.PublicIpAddress),
		}
		cmd = exec.Command(app, args...)
		_, err = cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] genesis.json uploaded\n", *instance.PublicIpAddress)
		}

		//ssh -o StrictHostKeyChecking=no -i ~/.ssh/TradoveCheckED25519US.pem ubuntu@13.56.188.247 '/home/ubuntu/bin/geth --datadir /home/ubuntu/data init /home/ubuntu/data/genesis.json'
		app = "ssh"
		args = []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			fmt.Sprintf("ubuntu@%s", *instance.PublicIpAddress),
			"/home/ubuntu/bin/geth --datadir /home/ubuntu/data init /home/ubuntu/data/genesis.json",
		}
		cmd = exec.Command(app, args...)
		_, err = cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] data directory initialized\n", *instance.PublicIpAddress)
		}

		//scp -i ~/.ssh/TradoveCheckED25519US.pem -o "StrictHostKeyChecking=no" /tmp/quorum-cluster/genesis.json ubuntu@54.173.147.146:/home/ubuntu/data
		// copy nodekey file into geth directory
		app = "scp"
		args = []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			filepath.Join(quorumClusterPath, fmt.Sprintf("%d/nodekey", index)),
			fmt.Sprintf("ubuntu@%s:/home/ubuntu/data/geth", *instance.PublicIpAddress),
		}
		cmd = exec.Command(app, args...)
		_, err = cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] nodekey uploaded\n", *instance.PublicIpAddress)
		}
	}
}

func executeBlockchain(count int) {
	instances := listInstances(false)
	if count != len(instances) {
		log.Fatal("execute blockchain count mismatch", len(instances), count)
	}
	for _, instance := range instances {
		app := "ssh"
		args := []string{
			"-i",
			useKey(),
			"-o",
			"StrictHostKeyChecking=no",
			fmt.Sprintf("ubuntu@%s", *instance.PublicIpAddress),
			fmt.Sprintf(
				"PRIVATE_CONFIG=ignore nohup /home/ubuntu/bin/geth --datadir /home/ubuntu/data"+
					" --nodiscover --istanbul.blockperiod 5 --syncmode full --mine --minerthreads 1"+
					" --networkid %d --maxpeers 20 --http --http.addr 0.0.0.0 "+
					" --txpool.accountslots 5000 --txpool.globalslots 100000"+
					" --txpool.accountqueue 5000 --txpool.globalqueue 100000"+
					" --http.api admin,db,eth,debug,miner,net,shh,txpool,personal,web3,quorum,istanbul"+
					" --emitcheckpoints --ws --ws.addr 0.0.0.0 --ws.port 8546 --ws.origins '*'"+
					" --ws.api admin,db,eth,debug,miner,net,shh,txpool,personal,web3,quorum,istanbul"+
					" --verbosity 3 > geth.log 2> geth.err < /dev/null &",
				chainID,
			),
		}
		cmd := exec.Command(app, args...)
		_, err := cmd.Output()
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Printf("[%s] blockchain node is up!\n", *instance.PublicIpAddress)
		}
	}
}

func generateQuorumConfig(count int) error {
	app := "istanbul"
	countStr := fmt.Sprintf("%d", count)
	args := []string{"setup", "--num", countStr, "--nodes", "--quorum", "--save", "--verbose"}
	cmd := exec.Command(app, args...)
	cmd.Dir = quorumClusterPath
	_, err := cmd.Output()
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	instances := listInstances(false)
	updateStaticNodesConfig(count, instances)
	uploadConfigFilesAndInit(instances)

	return nil
}
