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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/crossplane-contrib/provider-awspcluster/apis/pcluster/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sexec "k8s.io/utils/exec"
	fakeexec "k8s.io/utils/exec/testing"
)

// Unlike many Kubernetes projects Crossplane does not use third party testing
// libraries, per the common Go test review comments. Crossplane encourages the
// use of table driven unit tests. The tests of the crossplane-runtime project
// are representative of the testing style Crossplane encourages.
//
// https://github.com/golang/go/wiki/TestComments
// https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md#contributing-code

func makeCluster() *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.ClusterSpec{
			ForProvider: v1alpha1.ClusterParameters{
				Region:               "us-eastish",
				ClusterConfiguration: "Image:\n        Os: alinux2\n",
			},
		},
		Status: v1alpha1.ClusterStatus{},
	}
}

func readResourceFile(path string, errToReturn error) func() ([]byte, []byte, error) {
	b, err := os.ReadFile(filepath.Join("resources", path))
	if err != nil {
		panic(fmt.Sprintf("couldn't read file: %s", err))
	}

	return func() ([]byte, []byte, error) {
		return b, nil, errToReturn
	}
}

func TestObserve(t *testing.T) {
	type fields struct {
		executor fakeexec.FakeExec
		actions  []fakeexec.FakeCommandAction
	}

	type args struct {
		ctx context.Context
		mg  resource.Managed
	}

	type want struct {
		o   managed.ExternalObservation
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"resourceUpToDate": {
			args: args{
				ctx: context.Background(),
				mg:  makeCluster(),
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
				},
				err: nil,
			},
			fields: fields{
				executor: fakeexec.FakeExec{
					CommandScript: []fakeexec.FakeCommandAction{
						func(cmd string, args ...string) k8sexec.Cmd {
							return &fakeexec.FakeCmd{
								CombinedOutputScript: []fakeexec.FakeAction{
									readResourceFile("describeOutPut.json", nil),
								},
							}
						},
						func(cmd string, args ...string) k8sexec.Cmd {
							return &fakeexec.FakeCmd{
								CombinedOutputScript: []fakeexec.FakeAction{
									readResourceFile("upToDate.json", fmt.Errorf("error")),
								},
							}
						},
					},
				},
			},
		},
		"resourceNotUpToDate": {
			args: args{
				ctx: context.Background(),
				mg:  makeCluster(),
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				},
				err: nil,
			},
			fields: fields{
				executor: fakeexec.FakeExec{
					CommandScript: []fakeexec.FakeCommandAction{
						func(cmd string, args ...string) k8sexec.Cmd {
							return &fakeexec.FakeCmd{
								CombinedOutputScript: []fakeexec.FakeAction{
									readResourceFile("describeOutPut.json", nil),
								},
							}
						},
						func(cmd string, args ...string) k8sexec.Cmd {
							return &fakeexec.FakeCmd{
								CombinedOutputScript: []fakeexec.FakeAction{
									readResourceFile("notUpToDate.json", fmt.Errorf("error")),
								},
							}
						},
					},
				},
			},
		},
		"resourceDoesNotExist": {
			args: args{
				ctx: context.Background(),
				mg:  makeCluster(),
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists: false,
				},
				err: nil,
			},
			fields: fields{
				executor: fakeexec.FakeExec{
					CommandScript: []fakeexec.FakeCommandAction{
						func(cmd string, args ...string) k8sexec.Cmd {
							return &fakeexec.FakeCmd{
								CombinedOutputScript: []fakeexec.FakeAction{
									readResourceFile("notFound.json", fmt.Errorf("notused")),
								},
							}
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := external{executor: &tc.fields.executor, logger: logging.NewNopLogger()}
			got, err := e.Observe(tc.args.ctx, tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}
