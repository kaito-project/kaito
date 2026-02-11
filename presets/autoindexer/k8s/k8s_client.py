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
from datetime import UTC, datetime
from typing import Any

from kubernetes import client, config
from kubernetes.client.rest import ApiException

from autoindexer.config import AUTOINDEXER_NAME, NAMESPACE

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
        self.namespace = NAMESPACE
        self.autoindexer_name = AUTOINDEXER_NAME
        
        logger.info(f"AutoIndexer K8s client initialized for namespace: {self.namespace}, autoindexer: {self.autoindexer_name}")

    def get_autoindexer(self) -> dict[str, Any] | None:
        """
        Get an AutoIndexer CRD.
            
        Returns:
            Dict containing the AutoIndexer CRD or None if not found
        """
        self.autoindexer_name
        self.namespace

        if not self.autoindexer_name:
            logger.warning("No AutoIndexer name specified")
            return None
            
        try:
            response = self.custom_api.get_namespaced_custom_object(
                group=self.api_group,
                version=self.api_version,
                namespace=self.namespace,
                plural=self.plural,
                name=self.autoindexer_name
            )
            logger.debug(f"Retrieved AutoIndexer {self.namespace}/{self.autoindexer_name}")
            # logger.info(f"AutoIndexer details: {response}")
            return response
            
        except ApiException as e:
            if e.status == 404:
                logger.warning(f"AutoIndexer {self.namespace}/{self.autoindexer_name} not found")
                return None
            else:
                logger.error(f"Failed to get AutoIndexer {self.namespace}/{self.autoindexer_name}: {e}")
                raise

    def update_autoindexer_status(self, status_update: dict[str, Any], update_success_or_failure: bool = False) -> bool:
        """
        Update the status of an AutoIndexer CRD.
        
        Args:
            status_update: Dictionary containing status updates
            update_success_or_failure: Whether to update success/failure counters
            
        Returns:
            bool: True if successful, False otherwise
        """

        if not self.autoindexer_name:
            logger.warning("No AutoIndexer name specified for status update")
            return False
            
        try:
            # Get current AutoIndexer
            logger.info(f"Updating AutoIndexer {self.namespace}/{self.autoindexer_name} status with: {status_update}")
            current = self.get_autoindexer()
            if not current:
                logger.error(f"Cannot update status: AutoIndexer {self.namespace}/{self.autoindexer_name} not found")
                return False
            
            # Update status
            if "status" not in current:
                current["status"] = {}
            
            current["status"].update(status_update)

            if update_success_or_failure:
                if status_update.get("indexingPhase") == "Completed":
                    current["status"]["successfulIndexingCount"] = current["status"].get("successfulIndexingCount", 0) + 1
                elif status_update.get("indexingPhase") == "Failed":
                    current["status"]["errorIndexingCount"] = current["status"].get("errorIndexingCount", 0) + 1
            
            # Patch the status subresource
            self.custom_api.patch_namespaced_custom_object_status(
                group=self.api_group,
                version=self.api_version,
                namespace=self.namespace,
                plural=self.plural,
                name=self.autoindexer_name,
                body=current
            )

            logger.info(f"Updated AutoIndexer {self.namespace}/{self.autoindexer_name} status")
            return True
            
        except ApiException as e:
            logger.error(f"Failed to update AutoIndexer status: {e}")
            return False

    def add_status_condition(self, condition_type: str, status: str, reason: str, message: str) -> bool:
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
            
        Returns:
            bool: True if successful, False otherwise
        """

        if not self.autoindexer_name:
            logger.warning("No AutoIndexer name specified for condition update")
            return False
            
        try:
            # Get current AutoIndexer
            current = self.get_autoindexer()
            if not current:
                logger.error(f"Cannot add condition: AutoIndexer {self.namespace}/{self.autoindexer_name} not found")
                return False
            
            # Initialize status and conditions if they don't exist
            if "status" not in current:
                current["status"] = {}
            if "conditions" not in current["status"]:
                current["status"]["conditions"] = []
            
            # Create new condition
            now = datetime.now(UTC).isoformat().replace("+00:00", "Z")
            
            # Find existing condition of the same type
            conditions = current["status"]["conditions"]
            found = False
            for i, condition in enumerate(conditions):
                if condition.get("type") == condition_type:
                    # Update existing condition
                    transition_time = now if condition.get("status") != status else condition.get("lastTransitionTime", now)
                    conditions[i] = self._create_condition(condition_type, status, reason, message, transition_time)
                    found = True
                    break
            
            if not found:
                # Add new condition
                new_condition = self._create_condition(condition_type, status, reason, message, now)
                conditions.append(new_condition)
            
            # Update the status
            self.custom_api.patch_namespaced_custom_object_status(
                group=self.api_group,
                version=self.api_version,
                namespace=self.namespace,
                plural=self.plural,
                name=self.autoindexer_name,
                body=current
            )

            logger.info(f"Added condition '{condition_type}' to AutoIndexer {self.namespace}/{self.autoindexer_name}")
            return True
            
        except ApiException as e:
            logger.error(f"Failed to add condition to AutoIndexer: {e}")
            return False

    def _create_condition(self, condition_type: str, status: str, reason: str, message: str, last_transition_time: str | None = None) -> dict[str, str]:
        """
        Create a condition dictionary structure.
        
        Args:
            condition_type: Type of condition
            status: Status of condition ("True", "False", "Unknown")
            reason: Short reason for the condition
            message: Human readable message
            last_transition_time: Optional timestamp, defaults to current time
            
        Returns:
            dict: Condition structure
        """
        if last_transition_time is None:
            last_transition_time = datetime.now(UTC).isoformat().replace("+00:00", "Z")
            
        return {
            "type": condition_type,
            "status": status,
            "reason": reason,
            "message": message,
            "lastTransitionTime": last_transition_time
        }

    def update_indexing_progress(self, total_documents: int) -> bool:
        """
        Update indexing progress in the AutoIndexer status.
        
        Args:
            total_documents: Total number of documents processed
            
        Returns:
            bool: True if successful, False otherwise
        """
        progress_update = {
            "numOfDocumentInIndex": total_documents,
            "lastIndexingTimestamp": datetime.now(UTC).isoformat().replace("+00:00", "Z")
        }

        return self.update_autoindexer_status(progress_update)

    def update_indexing_phase(self, phase: str) -> bool:
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
            
        Returns:
            bool: True if successful, False otherwise
        """
        phase_update = {
            "indexingPhase": phase
        }

        return self.update_autoindexer_status(phase_update)

    def update_indexing_completion(self, success: bool, duration_seconds: int, document_count: int, commit_hash: str | None = None) -> bool:
        """
        Update status when indexing completes.
        
        Args:
            success: Whether indexing was successful
            duration_seconds: How long indexing took
            document_count: Number of documents indexed
            commit_hash: Git commit hash if applicable
            
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
            completion_update["successfulIndexingCount"] = self._increment_counter("successfulIndexingCount")
        else:
            completion_update["errorIndexingCount"] = self._increment_counter("errorIndexingCount")
        
        if commit_hash:
            completion_update["lastIndexedCommit"] = commit_hash

        return self.update_autoindexer_status(completion_update)

    def _increment_counter(self, counter_field: str) -> int:
        """Helper method to increment a counter field."""
        try:
            current = self.get_autoindexer()
            if current and "status" in current:
                current_count = current["status"].get(counter_field, 0)
                return current_count + 1
            return 1
        except Exception as e:
            logger.warning(f"Failed to get current counter value: {e}")
            return 1

    def get_autoindexer_config(self) -> dict[str, Any] | None:
        """
        Get the configuration from an AutoIndexer CRD spec.

        Returns:
            Dict containing the AutoIndexer spec or None if not found
        """
        autoindexer = self.get_autoindexer()
        if autoindexer:
            return autoindexer.get("spec", {})
        return None