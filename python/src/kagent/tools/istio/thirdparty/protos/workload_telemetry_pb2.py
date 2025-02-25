# -*- coding: utf-8 -*-
# Generated by the protocol buffer compiler.  DO NOT EDIT!
# NO CHECKED-IN PROTOBUF GENCODE
# source: workload-telemetry.proto
# Protobuf Python Version: 5.29.0
"""Generated protocol buffer code."""

from google.protobuf import descriptor as _descriptor
from google.protobuf import descriptor_pool as _descriptor_pool
from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2
from google.protobuf import runtime_version as _runtime_version
from google.protobuf import symbol_database as _symbol_database
from google.protobuf.internal import builder as _builder

_runtime_version.ValidateProtobufRuntimeVersion(
    _runtime_version.Domain.PUBLIC, 5, 29, 0, "", "workload-telemetry.proto"
)
# @@protoc_insertion_point(imports)

_sym_db = _symbol_database.Default()


DESCRIPTOR = _descriptor_pool.Default().AddSerializedFile(
    b'\n\x18workload-telemetry.proto\x12\x18istio.workload.telemetry\x1a\x1bgoogle/protobuf/empty.proto"\xd6\x01\n\x08TcpStats\x12\x13\n\x0bSocketState\x18\x01 \x01(\x05\x12\x18\n\x10TotalRetransmits\x18\x02 \x01(\x03\x12\x14\n\x0cTotalUnacked\x18\x03 \x01(\x03\x12\x11\n\tPacketRTT\x18\x04 \x01(\x03\x12\x15\n\rPacketMeanRTT\x18\x05 \x01(\x03\x12\x14\n\x0cRecvMSegSize\x18\x06 \x01(\x03\x12\x14\n\x0cSendMSegSize\x18\x07 \x01(\x03\x12\x16\n\x0eRetransTimeout\x18\t \x01(\x03\x12\x17\n\x0f\x43ongestionState\x18\x0b \x01(\x05"\xb3\x05\n\x10LifetimeCounters\x12\x15\n\renrollment_ts\x18\x01 \x01(\x04\x12\x1e\n\x16\x66irst_outbound_conn_ts\x18\x03 \x01(\x04\x12\x1b\n\x13outbound_conn_total\x18\x05 \x01(\x04\x12\x1d\n\x15\x66irst_inbound_conn_ts\x18\x07 \x01(\x04\x12\x1a\n\x12inbound_conn_total\x18\t \x01(\x04\x12#\n\x1b\x66irst_inbound_plain_conn_ts\x18\x0b \x01(\x04\x12 \n\x18inbound_plain_conn_total\x18\r \x01(\x04\x12$\n\x1coutbound_conn_complete_total\x18\x0f \x01(\x04\x12#\n\x1binbound_conn_complete_total\x18\x11 \x01(\x04\x12)\n!inbound_plain_conn_complete_total\x18\x13 \x01(\x04\x12-\n%outbound_conn_l4_policy_blocked_total\x18\x15 \x01(\x04\x12,\n$inbound_conn_l4_policy_blocked_total\x18\x17 \x01(\x04\x12"\n\x1aoutbound_conn_failed_total\x18\x19 \x01(\x04\x12!\n\x19inbound_conn_failed_total\x18\x1b \x01(\x04\x12\'\n\x1finbound_plain_conn_failed_total\x18\x1d \x01(\x04\x12 \n\x18outbound_pool_conn_total\x18\x1f \x01(\x04\x12!\n\x19outbound_pool_conn_active\x18! \x01(\x04\x12\x1f\n\x17inbound_pool_conn_total\x18# \x01(\x04\x12 \n\x18inbound_pool_conn_active\x18% \x01(\x04"\xa1\x0b\n\x0bL4ConnEvent\x12\x38\n\x04peer\x18\x01 \x01(\x0b\x32*.istio.workload.telemetry.L4ConnEvent.Peer\x12\x42\n\tdirection\x18\x02 \x01(\x0e\x32/.istio.workload.telemetry.L4ConnEvent.Direction\x12\x37\n\ttcp_stats\x18\x03 \x01(\x0b\x32".istio.workload.telemetry.TcpStatsH\x00\x12\x44\n\x06\x64\x65nied\x18\x04 \x01(\x0b\x32\x32.istio.workload.telemetry.L4ConnEvent.PolicyDeniedH\x01\x12>\n\x06\x66\x61iled\x18\x05 \x01(\x0b\x32,.istio.workload.telemetry.L4ConnEvent.FailedH\x01\x12\x44\n\tcompleted\x18\x06 \x01(\x0b\x32/.istio.workload.telemetry.L4ConnEvent.CompletedH\x01\x12\x38\n\x03new\x18\x07 \x01(\x0b\x32).istio.workload.telemetry.L4ConnEvent.NewH\x01\x12\x41\n\x08pool_new\x18\x08 \x01(\x0b\x32-.istio.workload.telemetry.L4ConnEvent.PoolNewH\x01\x12I\n\x0cpool_updated\x18\t \x01(\x0b\x32\x31.istio.workload.telemetry.L4ConnEvent.PoolUpdatedH\x01\x12M\n\x0epool_completed\x18\n \x01(\x0b\x32\x33.istio.workload.telemetry.L4ConnEvent.PoolCompletedH\x01\x12\x61\n\x19pool_stream_count_updated\x18\x0b \x01(\x0b\x32<.istio.workload.telemetry.L4ConnEvent.PoolStreamCountUpdatedH\x01\x12G\n\x11lifetime_counters\x18\x0c \x01(\x0b\x32*.istio.workload.telemetry.LifetimeCountersH\x02\x12\x0f\n\x07\x63onn_id\x18\r \x01(\x04\x1aJ\n\x04Peer\x12\x0c\n\x04name\x18\x01 \x01(\t\x12\x11\n\tnamespace\x18\x02 \x01(\t\x12\x0f\n\x07\x61\x64\x64ress\x18\x03 \x03(\t\x12\x10\n\x08identity\x18\x04 \x03(\t\x1a&\n\x0cPolicyDenied\x12\x16\n\x0e\x64\x65nial_message\x18\x01 \x01(\t\x1a!\n\x06\x46\x61iled\x12\x17\n\x0f\x66\x61ilure_message\x18\x01 \x01(\t\x1aK\n\x03New\x12\x44\n\nencryption\x18\x01 \x01(\x0e\x32\x30.istio.workload.telemetry.L4ConnEvent.Encryption\x1au\n\tCompleted\x12\x10\n\x08\x62ytes_tx\x18\x01 \x01(\x04\x12\x10\n\x08\x62ytes_rx\x18\x02 \x01(\x04\x12\x44\n\nencryption\x18\x03 \x01(\x0e\x32\x30.istio.workload.telemetry.L4ConnEvent.Encryption\x1a\t\n\x07PoolNew\x1a\r\n\x0bPoolUpdated\x1a\x0f\n\rPoolCompleted\x1aH\n\x16PoolStreamCountUpdated\x12\x14\n\x0cstream_count\x18\x02 \x01(\r\x12\x18\n\x10max_stream_count\x18\x03 \x01(\r"&\n\tDirection\x12\x0b\n\x07INBOUND\x10\x00\x12\x0c\n\x08OUTBOUND\x10\x01"#\n\nEncryption\x12\x0b\n\x07UNKNOWN\x10\x00\x12\x08\n\x04MTLS\x10\x01\x42\x07\n\x05statsB\t\n\x07\x63ontextB\n\n\x08\x63ounters"N\n\x05State\x12\x45\n\x11lifetime_counters\x18\x01 \x01(\x0b\x32*.istio.workload.telemetry.LifetimeCounters"\x8c\x01\n\rEventResponse\x12\x36\n\x05\x65vent\x18\x01 \x01(\x0b\x32%.istio.workload.telemetry.L4ConnEventH\x00\x12\x37\n\tlag_error\x18\x02 \x01(\x0b\x32".istio.workload.telemetry.LagErrorH\x00\x42\n\n\x08response"\x1e\n\x08LagError\x12\x12\n\nlost_count\x18\x01 \x01(\x04\x32\xa4\x01\n\x0eWorkloadEvents\x12M\n\x06\x45vents\x12\x16.google.protobuf.Empty\x1a\'.istio.workload.telemetry.EventResponse"\x00\x30\x01\x12\x43\n\x06Status\x12\x16.google.protobuf.Empty\x1a\x1f.istio.workload.telemetry.State"\x00\x42\x11Z\x0fpkg/workloadapib\x06proto3'
)

_globals = globals()
_builder.BuildMessageAndEnumDescriptors(DESCRIPTOR, _globals)
_builder.BuildTopDescriptorsAndMessages(DESCRIPTOR, "workload_telemetry_pb2", _globals)
if not _descriptor._USE_C_DESCRIPTORS:
    _globals["DESCRIPTOR"]._loaded_options = None
    _globals["DESCRIPTOR"]._serialized_options = b"Z\017pkg/workloadapi"
    _globals["_TCPSTATS"]._serialized_start = 84
    _globals["_TCPSTATS"]._serialized_end = 298
    _globals["_LIFETIMECOUNTERS"]._serialized_start = 301
    _globals["_LIFETIMECOUNTERS"]._serialized_end = 992
    _globals["_L4CONNEVENT"]._serialized_start = 995
    _globals["_L4CONNEVENT"]._serialized_end = 2436
    _globals["_L4CONNEVENT_PEER"]._serialized_start = 1865
    _globals["_L4CONNEVENT_PEER"]._serialized_end = 1939
    _globals["_L4CONNEVENT_POLICYDENIED"]._serialized_start = 1941
    _globals["_L4CONNEVENT_POLICYDENIED"]._serialized_end = 1979
    _globals["_L4CONNEVENT_FAILED"]._serialized_start = 1981
    _globals["_L4CONNEVENT_FAILED"]._serialized_end = 2014
    _globals["_L4CONNEVENT_NEW"]._serialized_start = 2016
    _globals["_L4CONNEVENT_NEW"]._serialized_end = 2091
    _globals["_L4CONNEVENT_COMPLETED"]._serialized_start = 2093
    _globals["_L4CONNEVENT_COMPLETED"]._serialized_end = 2210
    _globals["_L4CONNEVENT_POOLNEW"]._serialized_start = 2212
    _globals["_L4CONNEVENT_POOLNEW"]._serialized_end = 2221
    _globals["_L4CONNEVENT_POOLUPDATED"]._serialized_start = 2223
    _globals["_L4CONNEVENT_POOLUPDATED"]._serialized_end = 2236
    _globals["_L4CONNEVENT_POOLCOMPLETED"]._serialized_start = 2238
    _globals["_L4CONNEVENT_POOLCOMPLETED"]._serialized_end = 2253
    _globals["_L4CONNEVENT_POOLSTREAMCOUNTUPDATED"]._serialized_start = 2255
    _globals["_L4CONNEVENT_POOLSTREAMCOUNTUPDATED"]._serialized_end = 2327
    _globals["_L4CONNEVENT_DIRECTION"]._serialized_start = 2329
    _globals["_L4CONNEVENT_DIRECTION"]._serialized_end = 2367
    _globals["_L4CONNEVENT_ENCRYPTION"]._serialized_start = 2369
    _globals["_L4CONNEVENT_ENCRYPTION"]._serialized_end = 2404
    _globals["_STATE"]._serialized_start = 2438
    _globals["_STATE"]._serialized_end = 2516
    _globals["_EVENTRESPONSE"]._serialized_start = 2519
    _globals["_EVENTRESPONSE"]._serialized_end = 2659
    _globals["_LAGERROR"]._serialized_start = 2661
    _globals["_LAGERROR"]._serialized_end = 2691
    _globals["_WORKLOADEVENTS"]._serialized_start = 2694
    _globals["_WORKLOADEVENTS"]._serialized_end = 2858
# @@protoc_insertion_point(module_scope)
