/**
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package realis provides the ability to use Thrift API to communicate with Apache Aurora.
package realis

import (
	"encoding/base64"
	"fmt"
	"gen-go/apache/aurora"
	"git.apache.org/thrift.git/lib/go/thrift"
	"github.com/pkg/errors"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"
)

type Realis struct {
	client *aurora.AuroraSchedulerManagerClient
}

// Wrap object to provide future flexibility
type RealisConfig struct {
	transport thrift.TTransport
}

// Create a new Client with a default transport layer
func NewClient(config RealisConfig) *Realis {

	httpTrans := (config.transport).(*thrift.THttpClient)
	httpTrans.SetHeader("User-Agent", "GoRealis v0.1")

	protocolFactory := thrift.NewTJSONProtocolFactory()

	return &Realis{client: aurora.NewAuroraSchedulerManagerClientFactory(config.transport, protocolFactory)}
}

// Create a default configuration of the transport layer, requires a URL to test connection with.
func NewDefaultConfig(url string) (RealisConfig, error) {
	jar, err := cookiejar.New(nil)

	if err != nil {
		return RealisConfig{}, errors.Wrap(err, "Error creating Cookie Jar.")
	}

	//Custom client to timeout after 10 seconds to avoid hanging
	trans, err := thrift.NewTHttpPostClientWithOptions(url+"/api",
		thrift.THttpClientOptions{Client: &http.Client{Timeout: time.Second * 10, Jar: jar}})

	if err != nil {
		return RealisConfig{}, errors.Wrap(err, "Error creating transport.")
	}

	if err := trans.Open(); err != nil {
		fmt.Fprintln(os.Stderr)
		return RealisConfig{}, errors.Wrapf(err, "Error opening connection to %s.", url)
	}

	return RealisConfig{transport: trans}, nil

}

// Helper function to add basic authorization needed to communicate with Apache Aurora.
func AddBasicAuth(config *RealisConfig, username string, password string) {
	httpTrans := (config.transport).(*thrift.THttpClient)
	httpTrans.SetHeader("Authorization", "Basic "+basicAuth(username, password))
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// Releases resources associated with the realis client.
func (r *Realis) Close() {
	r.client.Transport.Close()
}

// Uses predefined set of states to retrieve a set of active jobs in Apache Aurora.
func (r *Realis) getActiveInstanceIds(key *aurora.JobKey) (map[int32]bool, error) {
	taskQ := &aurora.TaskQuery{Role: key.Role,
		Environment: key.Environment,
		JobName:     key.Name,
		Statuses:    aurora.ACTIVE_STATES}

	response, err := r.client.GetTasksWithoutConfigs(taskQ)
	if err != nil {
		return nil, errors.Wrap(err, "Error querying Aurora Scheduler")
	}

	tasks := response.GetResult_().GetScheduleStatusResult_().GetTasks()

	jobInstanceIds := make(map[int32]bool)
	for _, task := range tasks {
		jobInstanceIds[task.GetAssignedTask().GetInstanceId()] = true
	}

	return jobInstanceIds, nil
}

// Kill a specific instance of a job.
func (r *Realis) KillInstance(key *aurora.JobKey, instanceId int32) (*aurora.Response, error) {

	instanceIds := make(map[int32]bool)
	instanceIds[instanceId] = true

	response, err := r.client.KillTasks(key, instanceIds)

	if err != nil {
		return nil, errors.Wrap(err, "Error sending Kill command to Aurora Scheduler.")
	}

	return response, nil
}

// Sends a kill message to the scheduler for all active tasks under a job.
func (r *Realis) KillJob(key *aurora.JobKey) (*aurora.Response, error) {

	instanceIds, err := r.getActiveInstanceIds(key)
	if err != nil {
		return nil, errors.Wrap(err, "Could not retrieve relevant task instance IDs.")
	}

	if len(instanceIds) > 0 {
		response, err := r.client.KillTasks(key, instanceIds)

		if err != nil {
			return nil, errors.Wrap(err, "Error sending Kill command to Aurora Scheduler.")
		}

		return response, nil
	} else {
		return nil, errors.New("No tasks in the Active state.")
	}
}

// Sends a create job message to the scheduler with a specific job configuration.
func (r *Realis) CreateJob(auroraJob *Job) (*aurora.Response, error) {
	response, err := r.client.CreateJob(auroraJob.jobConfig)

	if err != nil {
		return nil, errors.Wrap(err, "Error sending Create command to Aurora Scheduler.")
	}

	return response, nil
}

// Restarts all active tasks under a job configuration.
func (r *Realis) RestartJob(key *aurora.JobKey) (*aurora.Response, error) {

	instanceIds, err := r.getActiveInstanceIds(key)
	if err != nil {
		return nil, errors.Wrap(err, "Could not retrieve relevant task instance IDs.")
	}

	if len(instanceIds) > 0 {
		response, err := r.client.RestartShards(key, instanceIds)

		if err != nil {
			return nil, errors.Wrap(err, "Error sending Restart command to Aurora Scheduler.")
		}

		return response, nil
	} else {
		return nil, errors.New("No tasks in the Active state.")
	}
}

// Update all tasks under a job configuration. Currently there's no support for canary deployments.
func (r *Realis) StartJobUpdate(updateJob *UpdateJob, message string) (*aurora.Response, error) {

	response, err := r.client.StartJobUpdate(updateJob.req, message)

	if err != nil {
		return nil, errors.Wrap(err, "Error sending StartJobUpdate command to Aurora Scheduler.")
	}

	return response, nil
}

// Abort Job Update on Aurora. Requires the updateId which can be obtained on the Aurora web UI.
func (r *Realis) AbortJobUpdate(
	key *aurora.JobKey,
	updateId string,
	message string) (*aurora.Response, error) {

	response, err := r.client.AbortJobUpdate(&aurora.JobUpdateKey{key, updateId}, message)

	if err != nil {
		return nil, errors.Wrap(err, "Error sending AbortJobUpdate command to Aurora Scheduler.")
	}

	return response, nil
}

// Scale up the number of instances under a job configuration using the configuration for specific
// instance to scale up.
func (r *Realis) AddInstances(instKey *aurora.InstanceKey, count int32) (*aurora.Response, error) {

	response, err := r.client.AddInstances(instKey, count)

	if err != nil {
		return nil, errors.Wrap(err, "Error sending AddInstances command to Aurora Scheduler.")
	}

	return response, nil
}
