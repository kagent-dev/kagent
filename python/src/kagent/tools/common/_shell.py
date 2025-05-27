import logging
import subprocess
from typing import List, Optional

from ._binary_paths import BinaryPathsConfig

logger = logging.getLogger(__name__)

# Global configuration instance
_binary_paths = BinaryPathsConfig()

def run_command(command: str, args: List[str]) -> str:
    """
    Run a shell command and return its output.
    
    Args:
        command: The command to run (e.g., 'kubectl', 'istioctl', 'helm')
        args: List of arguments to pass to the command
        
    Returns:
        The command output as a string
    """
    # Get the appropriate binary path based on the command
    binary_path = command
    if command == "kubectl":
        binary_path = _binary_paths.kubectl_path
    elif command == "istioctl":
        binary_path = _binary_paths.istioctl_path
    elif command == "helm":
        binary_path = _binary_paths.helm_path
        
    try:
        # Run the command with the specified binary path
        result = subprocess.run(
            [binary_path] + args,
            capture_output=True,
            text=True,
            check=True
        )
        return result.stdout
    except subprocess.CalledProcessError as e:
        logger.error(f"Command failed: {e.stderr}")
        raise
    except Exception as e:
        logger.error(f"Error running command: {str(e)}")
        raise
