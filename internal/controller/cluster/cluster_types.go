package cluster

import "time"

type OutputCluster struct {
	ClusterName               string        `json:"clusterName"`
	CloudformationStackStatus string        `json:"cloudformationStackStatus"`
	CloudformationStackArn    string        `json:"cloudformationStackArn"`
	ClusterStatus             string        `json:"clusterStatus"`
	Region                    string        `json:"region"`
	Version                   string        `json:"version"`
	Scheduler                 SchedulerType `json:"scheduler,omitempty"`
}

type Tag struct {
	Value string `json:"value"`
	Key   string `json:"key"`
}

type SchedulerType struct {
	SchedulerType string `json:"type"`
}

type DescribeClusterOutput struct {
	OutputCluster `json:"inline"`
	CreationTime  time.Time `json:"creationTime"`
	HeadNode      struct {
		LaunchTime       time.Time `json:"launchTime"`
		InstanceID       string    `json:"instanceId"`
		PublicIPAddress  string    `json:"publicIpAddress"`
		InstanceType     string    `json:"instanceType"`
		State            string    `json:"state"`
		PrivateIPAddress string    `json:"privateIpAddress"`
	} `json:"headNode"`
	//Version              string `json:"version"`
	ClusterConfiguration struct {
		URL string `json:"url"`
	} `json:"clusterConfiguration"`
	Tags []Tag `json:"tags"`
	//CloudFormationStackStatus string    `json:"cloudFormationStackStatus"`
	//ClusterName               string    `json:"clusterName"`
	ComputeFleetStatus string `json:"computeFleetStatus"`
	//CloudformationStackArn    string    `json:"cloudformationStackArn"`
	LastUpdatedTime time.Time `json:"lastUpdatedTime"`
	//Region                    string    `json:"region"`
	//ClusterStatus             string    `json:"clusterStatus"`
}

type CreateClusterOutput struct {
	Cluster OutputCluster `json:"cluster"`
}

type DeleteClusterOutput struct {
	Cluster OutputCluster `json:"cluster"`
}

type UpdateClusterOutput struct {
	Cluster OutputCluster `json:"cluster"`
}

type errorOutput struct {
	Message string `json:"message"`
}
