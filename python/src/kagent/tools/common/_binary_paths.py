from typing import Optional
from pydantic import BaseModel, Field

class BinaryPathsConfig(BaseModel):
    """Configuration for binary paths used by various tools."""
    
    kubectl_path: Optional[str] = Field(
        default="kubectl",
        description="Path to the kubectl binary. If not specified, will use 'kubectl' from PATH."
    )
    
    istioctl_path: Optional[str] = Field(
        default="istioctl",
        description="Path to the istioctl binary. If not specified, will use 'istioctl' from PATH."
    )
    
    helm_path: Optional[str] = Field(
        default="helm",
        description="Path to the helm binary. If not specified, will use 'helm' from PATH."
    ) 