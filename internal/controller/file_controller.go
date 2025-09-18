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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
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
	log := log.FromContext(ctx)

	// Fetch the File resource
	file := &githubv1alpha1.File{}
	err := r.Get(ctx, req.NamespacedName, file)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object deleted, nothing to do
			log.Info("File resource deleted", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get File resource")
		return ctrl.Result{}, err
	}

	log.Info("Reconciling File", "name", file.Name, "namespace", file.Namespace)

	// Define the finalizer name
	finalizerName := "github.example.com/file-finalizer"

	// Check if the File resource is being deleted
	if file.DeletionTimestamp != nil {
		// Resource is being deleted, handle cleanup
		return r.handleFileDeletion(ctx, file, finalizerName)
	}

	// Add finalizer if it doesn't exist
	if !containsString(file.Finalizers, finalizerName) {
		file.Finalizers = append(file.Finalizers, finalizerName)
		if err := r.Update(ctx, file); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		log.Info("Added finalizer to File resource")
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if GitHub client is available
	if r.GitHubClient == nil {
		err := fmt.Errorf("GitHub client not initialized")
		log.Error(err, "Cannot proceed without GitHub client")

		// Update status with error condition
		condition := r.createCondition("Available", "GitHubClientUnavailable",
			"GitHub client is not initialized", metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	// Validate required fields
	if file.Spec.RepositoryURL == "" {
		err := fmt.Errorf("repositoryURL is required")
		log.Error(err, "Invalid File spec")

		condition := r.createCondition("Available", "InvalidSpec",
			"repositoryURL field is required", metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, nil
	}

	if file.Spec.FilePath == "" {
		err := fmt.Errorf("filePath is required")
		log.Error(err, "Invalid File spec")

		condition := r.createCondition("Available", "InvalidSpec",
			"filePath field is required", metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, nil
	}

	if file.Spec.FileContent == "" {
		err := fmt.Errorf("fileContent is required")
		log.Error(err, "Invalid File spec")

		condition := r.createCondition("Available", "InvalidSpec",
			"fileContent field is required", metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, nil
	}

	// Set status to Progressing
	progressCondition := r.createCondition("Progressing", "DeployingFile",
		"Deploying file to GitHub repository", metav1.ConditionTrue)
	if err := r.updateFileStatus(ctx, file, progressCondition, "", ""); err != nil {
		log.Error(err, "Failed to update status to progressing")
	}

	// Deploy the file to GitHub
	deployed, err := r.deployFileToGitHub(ctx, file)
	if err != nil {
		log.Error(err, "Failed to deploy file to GitHub")

		// Update status with error condition
		condition := r.createCondition("Available", "DeploymentFailed",
			fmt.Sprintf("Failed to deploy file: %v", err), metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}

		// Requeue after some time to retry
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	// Only update status if deployment actually happened
	if deployed {
		// Update status with success condition
		successCondition := r.createCondition("Available", "DeploymentSuccessful",
			"File successfully deployed to GitHub repository", metav1.ConditionTrue)

		// Generate the file URL
		branch := file.Spec.Branch
		if branch == "" {
			branch = "main"
		}
		fileURL := fmt.Sprintf("https://github.com/%s/blob/%s/%s",
			file.Spec.RepositoryURL, branch, file.Spec.FilePath)

		if err := r.updateFileStatus(ctx, file, successCondition, "latest", fileURL); err != nil {
			log.Error(err, "Failed to update status with success")
			// Don't fail the reconciliation if status update fails
		}

		log.Info("Successfully reconciled File", "name", file.Name, "fileURL", fileURL)
	} else {
		log.Info("File already up to date, no deployment needed", "name", file.Name)
	}

	// Requeue after 30 minutes to check for any drift (less aggressive)
	return ctrl.Result{RequeueAfter: time.Minute * 4}, nil
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

// deployFileToGitHub deploys the YAML content to the specified GitHub repository
// Returns (deployed, error) where deployed indicates if a deployment actually occurred
func (r *FileReconciler) deployFileToGitHub(ctx context.Context, file *githubv1alpha1.File) (bool, error) {
	// Parse repository URL (format: owner/repo)
	repoParts := strings.Split(file.Spec.RepositoryURL, "/")
	if len(repoParts) != 2 {
		return false, fmt.Errorf("invalid repository URL format, expected 'owner/repo', got: %s", file.Spec.RepositoryURL)
	}
	owner, repo := repoParts[0], repoParts[1]

	// Validate owner and repo names
	if owner == "" || repo == "" {
		return false, fmt.Errorf("invalid repository URL: owner and repo cannot be empty")
	}

	// Set default branch if not specified
	branch := file.Spec.Branch
	if branch == "" {
		branch = "main"
	}

	// Set default commit message if not specified
	commitMessage := file.Spec.CommitMessage
	if commitMessage == "" {
		commitMessage = fmt.Sprintf("Deploy %s via Kubernetes File CRD", file.Spec.FilePath)
	}

	setupLog.Info("Deploying file to GitHub",
		"owner", owner,
		"repo", repo,
		"branch", branch,
		"filePath", file.Spec.FilePath)

	// First, validate that the repository exists and is accessible
	repoInfo, _, err := r.GitHubClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return false, fmt.Errorf("repository %s/%s not found or not accessible: %w", owner, repo, err)
	}

	// Check if the branch exists
	branchInfo, _, err := r.GitHubClient.Repositories.GetBranch(ctx, owner, repo, branch, 1)
	if err != nil {
		return false, fmt.Errorf("branch '%s' not found in repository %s/%s: %w", branch, owner, repo, err)
	}

	setupLog.Info("Repository and branch validated",
		"repoFullName", repoInfo.GetFullName(),
		"branchSHA", branchInfo.GetCommit().GetSHA())

	// Check if the file already exists
	existingFile, _, getResp, err := r.GitHubClient.Repositories.GetContents(
		ctx, owner, repo, file.Spec.FilePath, &github.RepositoryContentGetOptions{
			Ref: branch,
		})

	var sha *string
	var needsUpdate bool = true
	if err != nil && getResp.StatusCode != 404 {
		return false, fmt.Errorf("error checking if file exists: %w", err)
	}

	if existingFile != nil {
		sha = existingFile.SHA

		// Decode existing file content and compare with desired content
		existingContent, err := existingFile.GetContent()
		if err != nil {
			return false, fmt.Errorf("failed to decode existing file content: %w", err)
		}

		// Compare content to see if update is needed
		if strings.TrimSpace(existingContent) == strings.TrimSpace(file.Spec.FileContent) {
			setupLog.Info("File content is already up to date, skipping deployment", "sha", *sha)
			needsUpdate = false
		} else {
			setupLog.Info("File content differs, will update", "sha", *sha)
		}
	} else {
		setupLog.Info("File does not exist, will create new file")
	}

	// Skip deployment if content hasn't changed
	if !needsUpdate {
		return false, nil
	}

	// Validate file content is not empty and appears to be valid YAML
	if err := r.validateYAMLContent(file.Spec.FileContent); err != nil {
		return false, fmt.Errorf("invalid YAML content: %w", err)
	}

	// Prepare the file content
	contentBytes := []byte(file.Spec.FileContent)

	// Create or update the file
	fileContent := &github.RepositoryContentFileOptions{
		Message: &commitMessage,
		Content: contentBytes,
		Branch:  &branch,
		SHA:     sha,
		Committer: &github.CommitAuthor{
			Name:  github.String("Kubernetes File Controller"),
			Email: github.String("file-controller@k8s.local"),
		},
	}

	var result *github.RepositoryContentResponse
	var resp *github.Response
	if sha != nil {
		// Update existing file
		result, resp, err = r.GitHubClient.Repositories.UpdateFile(
			ctx, owner, repo, file.Spec.FilePath, fileContent)
	} else {
		// Create new file
		result, resp, err = r.GitHubClient.Repositories.CreateFile(
			ctx, owner, repo, file.Spec.FilePath, fileContent)
	}

	if err != nil {
		return false, fmt.Errorf("failed to create/update file in GitHub (status: %d): %w", resp.StatusCode, err)
	}

	setupLog.Info("Successfully deployed file to GitHub",
		"commit", result.Commit.GetSHA(),
		"fileURL", result.Content.GetHTMLURL())

	return true, nil
}

// validateYAMLContent performs basic validation on the YAML content
func (r *FileReconciler) validateYAMLContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("content cannot be empty")
	}

	// Basic YAML syntax validation - check for common issues
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Empty lines and comments are fine
		}

		// Check for basic YAML structure
		if strings.Contains(line, "apiVersion:") ||
			strings.Contains(line, "kind:") ||
			strings.Contains(line, "metadata:") ||
			strings.Contains(line, "spec:") ||
			strings.Contains(line, "data:") ||
			strings.HasSuffix(line, ":") ||
			strings.HasPrefix(line, "-") ||
			strings.Contains(line, ": ") {
			continue // Looks like YAML
		}

		// If we've seen 10+ lines and none look like YAML, it might not be YAML
		if i > 10 {
			setupLog.Info("Content validation warning: content may not be valid YAML", "line", i+1, "content", line)
			break
		}
	}

	return nil
}

// updateFileStatus updates the status of the File CRD with deployment information
func (r *FileReconciler) updateFileStatus(ctx context.Context, file *githubv1alpha1.File, condition metav1.Condition, commitSHA, fileURL string) error {
	// Use retry logic to handle conflicts
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest version of the resource
		latest := &githubv1alpha1.File{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(file), latest); err != nil {
			return err
		}

		// Update the status fields
		now := metav1.NewTime(time.Now())
		if commitSHA != "" {
			latest.Status.LastDeployedCommit = commitSHA
			latest.Status.LastDeployedTime = &now
		}
		if fileURL != "" {
			latest.Status.DeployedFileURL = fileURL
		}

		// Update or add the condition
		conditionExists := false
		for i, cond := range latest.Status.Conditions {
			if cond.Type == condition.Type {
				latest.Status.Conditions[i] = condition
				conditionExists = true
				break
			}
		}
		if !conditionExists {
			latest.Status.Conditions = append(latest.Status.Conditions, condition)
		}

		// Update the status in the cluster
		return r.Status().Update(ctx, latest)
	})
}

// createCondition creates a new condition for the File status
func (r *FileReconciler) createCondition(conditionType, reason, message string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

// handleFileDeletion handles the deletion of a File resource
func (r *FileReconciler) handleFileDeletion(ctx context.Context, file *githubv1alpha1.File, finalizerName string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Handling File deletion", "name", file.Name, "namespace", file.Namespace)

	// Perform cleanup logic here
	if err := r.deleteFileFromGitHub(ctx, file); err != nil {
		log.Error(err, "Failed to delete file from GitHub")
		// Update status with error condition
		condition := r.createCondition("Available", "DeletionFailed",
			fmt.Sprintf("Failed to delete file from GitHub: %v", err), metav1.ConditionFalse)
		if updateErr := r.updateFileStatus(ctx, file, condition, "", ""); updateErr != nil {
			log.Error(updateErr, "Failed to update status during deletion")
		}
		// Requeue to retry deletion
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	// Remove the finalizer to allow Kubernetes to delete the resource
	file.Finalizers = removeString(file.Finalizers, finalizerName)
	if err := r.Update(ctx, file); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Successfully cleaned up File resource and removed finalizer")
	return ctrl.Result{}, nil
}

// deleteFileFromGitHub deletes the file from the GitHub repository
func (r *FileReconciler) deleteFileFromGitHub(ctx context.Context, file *githubv1alpha1.File) error {
	// Check if GitHub client is available
	if r.GitHubClient == nil {
		return fmt.Errorf("GitHub client not initialized")
	}

	// Parse repository URL (format: owner/repo)
	repoParts := strings.Split(file.Spec.RepositoryURL, "/")
	if len(repoParts) != 2 {
		return fmt.Errorf("invalid repository URL format, expected 'owner/repo', got: %s", file.Spec.RepositoryURL)
	}
	owner, repo := repoParts[0], repoParts[1]

	// Set default branch if not specified
	branch := file.Spec.Branch
	if branch == "" {
		branch = "main"
	}

	setupLog.Info("Deleting file from GitHub",
		"owner", owner,
		"repo", repo,
		"branch", branch,
		"filePath", file.Spec.FilePath)

	// Check if the file exists
	existingFile, _, getResp, err := r.GitHubClient.Repositories.GetContents(
		ctx, owner, repo, file.Spec.FilePath, &github.RepositoryContentGetOptions{
			Ref: branch,
		})

	if err != nil {
		if getResp.StatusCode == 404 {
			setupLog.Info("File does not exist in GitHub, nothing to delete")
			return nil // File doesn't exist, consider it successfully "deleted"
		}
		return fmt.Errorf("error checking if file exists: %w", err)
	}

	// Delete the file
	commitMessage := fmt.Sprintf("Delete %s via Kubernetes File CRD", file.Spec.FilePath)
	deleteOptions := &github.RepositoryContentFileOptions{
		Message: &commitMessage,
		SHA:     existingFile.SHA,
		Branch:  &branch,
		Committer: &github.CommitAuthor{
			Name:  github.String("Kubernetes File Controller"),
			Email: github.String("file-controller@k8s.local"),
		},
	}

	_, _, err = r.GitHubClient.Repositories.DeleteFile(ctx, owner, repo, file.Spec.FilePath, deleteOptions)
	if err != nil {
		return fmt.Errorf("failed to delete file from GitHub: %w", err)
	}

	setupLog.Info("Successfully deleted file from GitHub", "filePath", file.Spec.FilePath)
	return nil
}

// containsString checks if a slice contains a specific string
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// removeString removes a specific string from a slice
func removeString(slice []string, item string) []string {
	result := make([]string, 0)
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
