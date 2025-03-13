import subprocess


# Function that runs the command in the shell
def run_command(command: str, args: list[str]) -> str:
    """Run the given command and return the output."""
    try:
        output = subprocess.check_output([command] + args, stderr=subprocess.STDOUT)
        return output.decode("utf-8")
    except subprocess.CalledProcessError as e:
        return f"Error running {command} command: {e.output.decode('utf-8')}"
    except FileNotFoundError:
        return "Error running {command} command: {command} not found in PATH"
