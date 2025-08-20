"""CLI for KAgent LangGraph package."""

import sys


def main():
    """Main CLI entry point."""
    print("KAgent LangGraph Integration v0.1.0")
    print("Use this package to create LangGraph agents with KAgent integration.")
    print("\nFor examples, see: python/samples/langgraph/basic/")
    
    if len(sys.argv) > 1:
        if sys.argv[1] == "--version":
            print("0.1.0")
        elif sys.argv[1] == "--help":
            print("\nUsage: kagent-langgraph [--version] [--help]")
            print("\nThis is a library package. See the samples for usage examples.")
        else:
            print(f"Unknown command: {sys.argv[1]}")
            sys.exit(1)


if __name__ == "__main__":
    main()
