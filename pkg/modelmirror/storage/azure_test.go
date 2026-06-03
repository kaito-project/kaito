// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import "testing"

func TestAzureBlobProvider_CSIDriverName(t *testing.T) {
	p := NewAzureBlobProvider()
	if got := p.CSIDriverName(); got != "blob.csi.azure.com" {
		t.Errorf("CSIDriverName() = %q, want %q", got, "blob.csi.azure.com")
	}
}

func TestAzureBlobProvider_ParseVolumeHandle(t *testing.T) {
	p := NewAzureBlobProvider()
	tests := []struct {
		name          string
		volumeHandle  string
		wantAccount   string
		wantContainer string
		wantErr       bool
	}{
		{
			name:          "standard AKS dynamic",
			volumeHandle:  "MC_rg_aks_eastus#storageacct1#container1##default#",
			wantAccount:   "storageacct1",
			wantContainer: "container1",
		},
		{
			name:          "custom resource group",
			volumeHandle:  "myRG#myaccount#mycontainer##ns1#",
			wantAccount:   "myaccount",
			wantContainer: "mycontainer",
		},
		{
			name:         "too few parts",
			volumeHandle: "onlyone",
			wantErr:      true,
		},
		{
			name:         "empty account",
			volumeHandle: "rg##container##ns#",
			wantErr:      true,
		},
		{
			name:         "empty container",
			volumeHandle: "rg#account###ns#",
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct, cont, err := p.ParseVolumeHandle(tt.volumeHandle)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if err == nil {
				if acct != tt.wantAccount {
					t.Errorf("account: got %q, want %q", acct, tt.wantAccount)
				}
				if cont != tt.wantContainer {
					t.Errorf("container: got %q, want %q", cont, tt.wantContainer)
				}
			}
		})
	}
}

func TestAzureBlobProvider_BuildStorageURI(t *testing.T) {
	p := NewAzureBlobProvider()
	uri := p.BuildStorageURI("mycontainer", "Qwen/Qwen2.5-Coder-32B-Instruct")
	want := "az://mycontainer/Qwen/Qwen2.5-Coder-32B-Instruct"
	if uri != want {
		t.Errorf("got %q, want %q", uri, want)
	}
}
