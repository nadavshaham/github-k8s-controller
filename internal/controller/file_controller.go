/*
Copyright 2025.

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

package controller

import (
	"context"
	"os"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	githubv1alpha1 "github.com/example/memcached-operator/api/v1alpha1"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

// initGitHubClient initializes a GitHub client using the API token from environment variable
func initGitHubClient(ctx context.Context) (*github.Client, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		setupLog.Error(nil, "GITHUB_TOKEN environment variable is not set")
		return nil, nil
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	// Test the connection by fetching user info
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		setupLog.Error(err, "Failed to connect to GitHub API")
		return nil, err
	}

	setupLog.Info("Successfully connected to GitHub", "user", user.GetLogin())
	return client, nil
}

// FileReconciler reconciles a File object
type FileReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	GitHubClient *github.Client
}

// +kubebuilder:rbac:groups=github.example.com,resources=files,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=github.example.com,resources=files/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=github.example.com,resources=files/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the File object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
func (r *FileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	file := &githubv1alpha1.File{}
	err := r.Get(ctx, req.NamespacedName, file)
	setupLog.Info("reconciling file", "file", file.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object deleted, nothing to do
			setupLog.Info("deleting file", "file", file.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	// Initialize GitHub client
	githubClient, err := initGitHubClient(ctx)
	if err != nil {
		return err
	}
	r.GitHubClient = githubClient

	// Test GitHub connection by fetching repositories
	if r.GitHubClient != nil {
		go r.testGitHubConnection(ctx)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&githubv1alpha1.File{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// testGitHubConnection tests the GitHub connection by fetching repositories
func (r *FileReconciler) testGitHubConnection(ctx context.Context) {
	setupLog.Info("Testing GitHub connection by fetching repositories...")

	// Fetch the first page of repositories for the authenticated user
	repos, _, err := r.GitHubClient.Repositories.List(ctx, "", &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 10},
	})
	if err != nil {
		setupLog.Error(err, "Failed to fetch repositories from GitHub")
		return
	}

	setupLog.Info("Successfully fetched repositories", "count", len(repos))
	for _, repo := range repos {
		setupLog.Info("Repository found", "name", repo.GetName(), "full_name", repo.GetFullName())
	}
}
