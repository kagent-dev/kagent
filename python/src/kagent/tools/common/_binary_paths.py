from typing import Optional
from pydantic import BaseModel, Field

class BinaryPathsConfig(BaseModel):
    """Configuration for binary paths used by various tools."""
    
    kubectl_path: Optional[str] = Field(
        default="/usr/local/bin/kubectl",
        description="Path to the kubectl binary. If not specified, will use '/usr/local/bin/kubectl'."
    )
    
    istioctl_path: Optional[str] = Field(
        default="/usr/local/bin/istioctl",
        description="Path to the istioctl binary. If not specified, will use '/usr/local/bin/istioctl'."
    )
    
    helm_path: Optional[str] = Field(
        default="/usr/local/bin/helm",
        description="Path to the helm binary. If not specified, will use '/usr/local/bin/helm'."
    )

    kubectl_argo_rollouts_path: Optional[str] = Field(
        default="/usr/local/bin/kubectl-argo-rollouts",
        description="Path to the kubectl-argo-rollouts binary. If not specified, will use '/usr/local/bin/kubectl-argo-rollouts'."
    ) 