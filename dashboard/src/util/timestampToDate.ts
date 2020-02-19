import * as timestamp_pb from 'google-protobuf/google/protobuf/timestamp_pb';

export default function timestampToDate(timestamp: timestamp_pb.Timestamp | undefined): Date | undefined {
	if (!timestamp) return undefined;
	return timestamp.toDate();
}
