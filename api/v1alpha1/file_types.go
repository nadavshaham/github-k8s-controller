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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FileSpec defines the desired state of File
type FileSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// FileContent defines the YAML content to be deployed to the repository
	// This should contain valid YAML that will be written to the specified file path
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	FileContent string `json:"fileContent"`

	// RepositoryURL defines the URL of the git repository where the file should be deployed
	// Format: owner/repo (e.g., "myorg/myrepo")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	RepositoryURL string `json:"repositoryURL"`

	// Branch defines the branch of the git repository where the file should be deployed
	// If not specified, defaults to "main"
	// +kubebuilder:default="main"
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Branch string `json:"branch,omitempty"`

	// FilePath defines the path where the file should be created in the repository
	// Should include the filename and extension (e.g., "config/deployment.yaml")
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	FilePath string `json:"filePath"`

	// CommitMessage defines the commit message to use when creating/updating the file
	// If not specified, a default message will be generated
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	CommitMessage string `json:"commitMessage,omitempty"`
}

// FileStatus defines the observed state of File
type FileStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions store the status conditions of the File deployment
	// Condition types are: "Available", "Progressing", and "Degraded"
	// Condition status can be True, False, or Unknown
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// LastDeployedCommit contains the SHA of the commit that was last deployed
	// +operator-sdk:csv:customresourcedefinitions:type=status
	LastDeployedCommit string `json:"lastDeployedCommit,omitempty"`

	// LastDeployedTime contains the timestamp of the last successful deployment
	// +operator-sdk:csv:customresourcedefinitions:type=status
	LastDeployedTime *metav1.Time `json:"lastDeployedTime,omitempty"`

	// DeployedFileURL contains the URL to the deployed file in the GitHub repository
	// +operator-sdk:csv:customresourcedefinitions:type=status
	DeployedFileURL string `json:"deployedFileURL,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// File is the Schema for the files API
type File struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FileSpec   `json:"spec,omitempty"`
	Status FileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FileList contains a list of File
type FileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []File `json:"items"`
}

func init() {
	SchemeBuilder.Register(&File{}, &FileList{})
}
