/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	k8sexec "k8s.io/utils/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane-contrib/provider-awspcluster/apis/pcluster/v1alpha1"
	apisv1alpha1 "github.com/crossplane-contrib/provider-awspcluster/apis/v1alpha1"
	"github.com/crossplane-contrib/provider-awspcluster/internal/controller/features"
)

const (
	clusterConfigFileName = "cluster-config.yaml"

	errNotCluster   = "managed resource is not a Cluster custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errNewClient                    = "cannot create new Service"
	virtualEnvPath                  = "PYTHON_VENV_PATH"
	CreateInProgress PClusterStatus = "CREATE_IN_PROGRESS"
	CreateFailed     PClusterStatus = "CREATE_FAILED"
	CreateComplete   PClusterStatus = "CREATE_COMPLETE"
	DeleteInProgress PClusterStatus = "DELETE_IN_PROGRESS"
	DeleteFailed     PClusterStatus = "DELETE_FAILED"
	DeleteComplete   PClusterStatus = "DELETE_COMPLETE"
	UpdateInProgress PClusterStatus = "UPDATE_IN_PROGRESS"
	UpdateComplete   PClusterStatus = "UPDATE_COMPLETE"
	UpdateFailed     PClusterStatus = "UPDATE_FAILED"

	errStatusNotFound errStatus = "clusterNotFound"
	errStatusEmpty    errStatus = "emptyMessage"
)

// A NoOpService does nothing.
type NoOpService struct{}

type errStatus = string

type PClusterStatus = string

var (
	newNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles Cluster managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.ClusterGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.ClusterGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newExectuor}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&v1alpha1.Cluster{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte) (k8sexec.Interface, error)
}

func newExectuor(creds []byte) (k8sexec.Interface, error) {
	return k8sexec.New(), nil
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.Cluster)
	if !ok {
		return nil, errors.New(errNotCluster)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	svc, err := c.newServiceFn(data)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}
	path, err := getVEnvPath()
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), fmt.Sprintf("PATH=%s", path))
	return &external{env: env, path: path, executor: svc}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	dir      string
	env      []string
	path     string
	executor k8sexec.Interface
}

func (c *external) execPcluster(ctx context.Context, cr *v1alpha1.Cluster, args ...string) ([]byte, error) {
	err := os.Setenv("PATH", c.path)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to set PATH: %w", err)
	}
	cmd := c.executor.CommandContext(ctx, "pcluster", args...)
	cmd.SetEnv(c.env)
	cmd.SetDir(c.dir)
	return cmd.CombinedOutput() // blocks
}

func (c *external) execute(ctx context.Context, cr *v1alpha1.Cluster, args []string) ([]byte, error) {
	dir, err := createTempDir(cr.Name)
	if err != nil {
		return []byte{}, err
	}
	defer os.RemoveAll(dir)

	c.dir = dir
	err = writeConfigToFile(cr.Spec.ForProvider.ClusterConfiguration, fmt.Sprintf("%s/%s", dir, clusterConfigFileName))
	if err != nil {
		return []byte{}, err
	}
	output, err := c.execPcluster(ctx, cr, args...)
	if err != nil {
		return []byte{}, err
	}
	return output, nil
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.Cluster)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotCluster)
	}
	output, err := c.execPcluster(ctx, cr, "describe-cluster", "--cluster-name", cr.Name)
	if err != nil {
		status, _ := getErrorStatus(output, cr.Name)
		if status == errStatusNotFound {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, fmt.Errorf("failed to run pcluster command: %s %w", output, err)
	}
	var describeOutput DescribeClusterOutput
	err = json.Unmarshal(output, &describeOutput.OutputCluster) // TODO avoid double unmarshal
	err = json.Unmarshal(output, &describeOutput)
	if err != nil {
		return managed.ExternalObservation{}, fmt.Errorf("failed to unmarshal describe response: %w", err)
	}

	cr.SetConditions(xpv1.Unavailable())

	eo := managed.ExternalObservation{}
	switch describeOutput.ClusterStatus {
	case CreateInProgress, UpdateInProgress:
		eo.ResourceExists = true
		eo.ResourceUpToDate = true
	case CreateComplete, UpdateComplete:
		eo.ResourceExists = true
		eo.ResourceUpToDate = true
		cr.SetConditions(xpv1.Available())
	case DeleteComplete, CreateFailed:
		eo.ResourceExists = false
		eo.ResourceUpToDate = false
	case UpdateFailed, DeleteFailed:
		eo.ResourceExists = true
		eo.ResourceUpToDate = false
	case DeleteInProgress:
		eo.ResourceExists = true
		eo.ResourceUpToDate = true
	}

	return eo, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.Cluster)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotCluster)
	}

	fmt.Printf("Creating: %+v", cr)
	args := []string{
		"create-cluster",
		"--cluster-configuration",
		clusterConfigFileName,
		"--cluster-name",
		cr.Name,
		"--region",
		cr.Spec.ForProvider.Region,
	}
	output, err := c.execute(ctx, cr, args)
	if err != nil {
		return managed.ExternalCreation{}, err
	}
	var createOutput CreateClusterOutput
	err = json.Unmarshal(output, &createOutput)
	if err != nil {
		return managed.ExternalCreation{}, fmt.Errorf("failed to unmarshal create output: %w", err)
	}
	setStatus(createOutput.Cluster, cr)

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.Cluster)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotCluster)
	}

	fmt.Printf("Updating: %+v", cr)
	args := []string{
		"update-cluster",
		"--cluster-configuration",
		clusterConfigFileName,
		"--cluster-name",
		cr.Name,
		"--region",
		cr.Spec.ForProvider.Region,
	}
	output, err := c.execute(ctx, cr, args)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	var updateOutput UpdateClusterOutput
	err = json.Unmarshal(output, &updateOutput)
	if err != nil {
		return managed.ExternalUpdate{}, fmt.Errorf("failed to unmarshal update output: %w", err)
	}
	setStatus(updateOutput.Cluster, cr)
	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.Cluster)
	if !ok {
		return errors.New(errNotCluster)
	}

	fmt.Printf("Deleting: %+v", cr)
	args := []string{
		"delete-cluster",
		"--cluster-name",
		cr.Name,
		"--region",
		cr.Spec.ForProvider.Region,
	}
	output, err := c.execute(ctx, cr, args)
	if err != nil {
		return err
	}
	var deleteOutput DeleteClusterOutput
	err = json.Unmarshal(output, &deleteOutput)
	if err != nil {
		return fmt.Errorf("failed to unmarshal update output: %w", err)
	}
	setStatus(deleteOutput.Cluster, cr)
	return nil
}

func getVEnvPath() (string, error) {
	vEnvPath, ok := os.LookupEnv(virtualEnvPath)
	if !ok {
		return "", nil
	}

	_, err := os.Stat(fmt.Sprintf("%s/bin/pcluster", vEnvPath))
	if err != nil {
		return "", fmt.Errorf("pcluster file not found: %w", err)
	}
	//cmd.Dir = vEnvPath
	virtEnvPath := fmt.Sprintf("%s/bin:%s", vEnvPath, os.Getenv("PATH"))
	return virtEnvPath, nil
}

func getErrorStatus(cmdOutput []byte, clusterName string) (errStatus, error) {
	var pErr errorOutput
	err := json.Unmarshal(cmdOutput, &pErr)
	if err != nil {
		return "", err
	}
	msg := pErr.Message
	switch {
	case strings.HasPrefix(msg, fmt.Sprintf("Cluster '%s' does not exist", clusterName)):
		return errStatusNotFound, nil
	default:
		return errStatusEmpty, nil
	}
}

func createTempDir(prefix string) (string, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("failed to create tmp dir: %w", err)
	}
	return dir, nil
}

func writeConfigToFile(input string, filePath string) error {
	configFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer configFile.Close()
	_, err = configFile.Write([]byte(input))
	if err != nil {
		return fmt.Errorf("failed to write to config file: %w", err)
	}
	return nil
}

func setStatus(output OutputCluster, cluster *v1alpha1.Cluster) {
	cluster.Status.AtProvider.ClusterStatus = output.ClusterStatus
	cluster.Status.AtProvider.CloudformationStackArn = output.CloudformationStackArn
	cluster.Status.AtProvider.Scheduler.SchedulerType = output.Scheduler.SchedulerType
	cluster.Status.AtProvider.ClusterName = output.ClusterName
}
