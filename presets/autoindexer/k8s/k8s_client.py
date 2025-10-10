# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


"""
Kubernetes client for interacting with KAITO AutoIndexer CRDs.
"""

import logging
import os
from datetime import UTC, datetime
from typing import Any

from kubernetes import client, config
from kubernetes.client.rest import ApiException

logger = logging.getLogger(__name__)


class AutoIndexerK8sClient:
    """
    Kubernetes client for interacting with AutoIndexer CRDs.
    """

    def __init__(self):
        """Initialize the Kubernetes client."""
        self.api_group = "kaito.sh"
        self.api_version = "v1alpha1"
        self.plural = "autoindexers"
        self.kind = "AutoIndexer"
        
        # Initialize Kubernetes client
        try:
            # Try to load in-cluster config first (when running in a pod)
            config.load_incluster_config()
            logger.info("Loaded in-cluster Kubernetes configuration")
        except config.ConfigException:
            try:
                # Fall back to kubeconfig for local development
                config.load_kube_config()
                logger.info("Loaded kubeconfig Kubernetes configuration")
            except config.ConfigException as e:
                logger.error(f"Failed to load Kubernetes configuration: {e}")
                raise
        
        self.custom_api = client.CustomObjectsApi()
        self.core_api = client.CoreV1Api()
        
        # Get current pod information if available
        self.namespace = self._get_current_namespace()
        self.autoindexer_name = self._get_autoindexer_name()
        
        logger.info(f"AutoIndexer K8s client initialized for namespace: {self.namespace}, autoindexer: {self.autoindexer_name}")

    def _get_current_namespace(self) -> str:
        """Get the current namespace from environment or service account."""
        # First try environment variable
        namespace = os.getenv("NAMESPACE")
        if namespace:
            return namespace
            
        # Try to read from service account token
        try:
            with open("/var/run/secrets/kubernetes.io/serviceaccount/namespace") as f:
                return f.read().strip()
        except FileNotFoundError:
            logger.warning("Could not determine namespace, using 'default'")
            return "default"

    def _get_autoindexer_name(self) -> str | None:
        """Get the AutoIndexer name from environment variable."""
        return os.getenv("AUTOINDEXER_NAME")

    def get_autoindexer(self, name: str | None = None, namespace: str | None = None) -> dict[str, Any] | None:
        """
        Get an AutoIndexer CRD.
        
        Args:
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            Dict containing the AutoIndexer CRD or None if not found
        """
        name = name or self.autoindexer_name
        namespace = namespace or self.namespace
        
        if not name:
            logger.warning("No AutoIndexer name specified")
            return None
            
        try:
            response = self.custom_api.get_namespaced_custom_object(
                group=self.api_group,
                version=self.api_version,
                namespace=namespace,
                plural=self.plural,
                name=name
            )
            logger.debug(f"Retrieved AutoIndexer {namespace}/{name}")
            return response
            
        except ApiException as e:
            if e.status == 404:
                logger.warning(f"AutoIndexer {namespace}/{name} not found")
                return None
            else:
                logger.error(f"Failed to get AutoIndexer {namespace}/{name}: {e}")
                raise

    def update_autoindexer_status(self, status_update: dict[str, Any], name: str | None = None, namespace: str | None = None) -> bool:
        """
        Update the status of an AutoIndexer CRD.
        
        Args:
            status_update: Dictionary containing status updates
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            bool: True if successful, False otherwise
        """
        name = name or self.autoindexer_name
        namespace = namespace or self.namespace
        
        if not name:
            logger.warning("No AutoIndexer name specified for status update")
            return False
            
        try:
            # Get current AutoIndexer
            current = self.get_autoindexer(name, namespace)
            if not current:
                logger.error(f"Cannot update status: AutoIndexer {namespace}/{name} not found")
                return False
            
            # Update status
            if "status" not in current:
                current["status"] = {}
            
            current["status"].update(status_update)
            
            # Patch the status subresource
            response = self.custom_api.patch_namespaced_custom_object_status(
                group=self.api_group,
                version=self.api_version,
                namespace=namespace,
                plural=self.plural,
                name=name,
                body=current
            )
            
            logger.info(f"Updated AutoIndexer {namespace}/{name} status")
            return True
            
        except ApiException as e:
            logger.error(f"Failed to update AutoIndexer status: {e}")
            return False

    def add_status_condition(self, condition_type: str, status: str, reason: str, message: str, name: str | None = None, namespace: str | None = None) -> bool:
        """
        Add or update a status condition on the AutoIndexer.
        
        Args:
            condition_type: Type of condition - should be one of:
                - "AutoIndexerSucceeded" 
                - "AutoIndexerScheduled"
                - "AutoIndexerIndexing" 
                - "AutoIndexerError"
                - "ResourceReady"
            status: Status of condition ("True", "False", "Unknown")
            reason: Short reason for the condition
            message: Human readable message
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            bool: True if successful, False otherwise
        """
        name = name or self.autoindexer_name
        namespace = namespace or self.namespace
        
        if not name:
            logger.warning("No AutoIndexer name specified for condition update")
            return False
            
        try:
            # Get current AutoIndexer
            current = self.get_autoindexer(name, namespace)
            if not current:
                logger.error(f"Cannot add condition: AutoIndexer {namespace}/{name} not found")
                return False
            
            # Initialize status and conditions if they don't exist
            if "status" not in current:
                current["status"] = {}
            if "conditions" not in current["status"]:
                current["status"]["conditions"] = []
            
            # Create new condition
            now = datetime.now(UTC).isoformat().replace("+00:00", "Z")
            new_condition = {
                "type": condition_type,
                "status": status,
                "reason": reason,
                "message": message,
                "lastTransitionTime": now
            }
            
            # Find existing condition of the same type
            conditions = current["status"]["conditions"]
            found = False
            for i, condition in enumerate(conditions):
                if condition.get("type") == condition_type:
                    # Update existing condition
                    if condition.get("status") != status:
                        new_condition["lastTransitionTime"] = now
                    else:
                        new_condition["lastTransitionTime"] = condition.get("lastTransitionTime", now)
                    conditions[i] = new_condition
                    found = True
                    break
            
            if not found:
                # Add new condition
                conditions.append(new_condition)
            
            # Update the status
            response = self.custom_api.patch_namespaced_custom_object_status(
                group=self.api_group,
                version=self.api_version,
                namespace=namespace,
                plural=self.plural,
                name=name,
                body=current
            )
            
            logger.info(f"Added condition '{condition_type}' to AutoIndexer {namespace}/{name}")
            return True
            
        except ApiException as e:
            logger.error(f"Failed to add condition to AutoIndexer: {e}")
            return False

    def update_indexing_progress(self, total_documents: int, processed_documents: int, name: str | None = None, namespace: str | None = None) -> bool:
        """
        Update indexing progress in the AutoIndexer status.
        
        Args:
            total_documents: Total number of documents to process
            processed_documents: Number of documents processed so far
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            bool: True if successful, False otherwise
        """
        progress_update = {
            "numOfDocumentInIndex": processed_documents,
            "lastIndexingTimestamp": datetime.now(UTC).isoformat().replace("+00:00", "Z")
        }
        
        return self.update_autoindexer_status(progress_update, name, namespace)

    def update_indexing_phase(self, phase: str, name: str | None = None, namespace: str | None = None) -> bool:
        """
        Update the indexing phase in the AutoIndexer status.
        
        Args:
            phase: Indexing phase - should be one of:
                - "Pending"
                - "Running" 
                - "Completed"
                - "Failed"
                - "Retrying"
                - "Unknown"
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            bool: True if successful, False otherwise
        """
        phase_update = {
            "indexingPhase": phase
        }
        
        return self.update_autoindexer_status(phase_update, name, namespace)

    def update_indexing_completion(self, success: bool, duration_seconds: int, document_count: int, 
                                   commit_hash: str | None = None, name: str | None = None, namespace: str | None = None) -> bool:
        """
        Update status when indexing completes.
        
        Args:
            success: Whether indexing was successful
            duration_seconds: How long indexing took
            document_count: Number of documents indexed
            commit_hash: Git commit hash if applicable
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            bool: True if successful, False otherwise
        """
        now = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        
        completion_update = {
            "lastIndexingTimestamp": now,
            "lastIndexingDurationSeconds": duration_seconds,
            "numOfDocumentInIndex": document_count,
            "indexingPhase": "Completed" if success else "Failed"
        }
        
        if success:
            completion_update["successfulIndexingCount"] = self._increment_counter("successfulIndexingCount", name, namespace)
        else:
            completion_update["errorIndexingCount"] = self._increment_counter("errorIndexingCount", name, namespace)
        
        if commit_hash:
            completion_update["lastIndexedCommit"] = commit_hash
        
        return self.update_autoindexer_status(completion_update, name, namespace)

    def _increment_counter(self, counter_field: str, name: str | None = None, namespace: str | None = None) -> int:
        """Helper method to increment a counter field."""
        try:
            current = self.get_autoindexer(name, namespace)
            if current and "status" in current:
                current_count = current["status"].get(counter_field, 0)
                return current_count + 1
            return 1
        except Exception as e:
            logger.warning(f"Failed to get current counter value: {e}")
            return 1

    def get_autoindexer_config(self, name: str | None = None, namespace: str | None = None) -> dict[str, Any] | None:
        """
        Get the configuration from an AutoIndexer CRD spec.
        
        Args:
            name: AutoIndexer name (defaults to current autoindexer)
            namespace: Namespace (defaults to current namespace)
            
        Returns:
            Dict containing the AutoIndexer spec or None if not found
        """
        autoindexer = self.get_autoindexer(name, namespace)
        if autoindexer:
            return autoindexer.get("spec", {})
        return None