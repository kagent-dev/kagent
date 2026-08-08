from google.protobuf.timestamp_pb2 import Timestamp


def now_timestamp() -> Timestamp:
    """Return a protobuf Timestamp set to the current UTC time."""
    ts = Timestamp()
    ts.GetCurrentTime()
    return ts
