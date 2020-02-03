// package: flynn.api.v1
// file: controller.proto

import * as jspb from "google-protobuf";
import * as google_protobuf_timestamp_pb from "google-protobuf/google/protobuf/timestamp_pb";
import * as google_protobuf_duration_pb from "google-protobuf/google/protobuf/duration_pb";
import * as google_protobuf_field_mask_pb from "google-protobuf/google/protobuf/field_mask_pb";
import * as google_protobuf_empty_pb from "google-protobuf/google/protobuf/empty_pb";

export class StatusResponse extends jspb.Message {
  getStatus(): StatusResponse.CodeMap[keyof StatusResponse.CodeMap];
  setStatus(value: StatusResponse.CodeMap[keyof StatusResponse.CodeMap]): void;

  getDetail(): Uint8Array | string;
  getDetail_asU8(): Uint8Array;
  getDetail_asB64(): string;
  setDetail(value: Uint8Array | string): void;

  getVersion(): string;
  setVersion(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StatusResponse.AsObject;
  static toObject(includeInstance: boolean, msg: StatusResponse): StatusResponse.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StatusResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StatusResponse;
  static deserializeBinaryFromReader(message: StatusResponse, reader: jspb.BinaryReader): StatusResponse;
}

export namespace StatusResponse {
  export type AsObject = {
    status: StatusResponse.CodeMap[keyof StatusResponse.CodeMap],
    detail: Uint8Array | string,
    version: string,
  }

  export interface CodeMap {
    HEALTHY: 0;
    UNHEALTHY: 1;
  }

  export const Code: CodeMap;
}

export class LabelFilter extends jspb.Message {
  clearExpressionsList(): void;
  getExpressionsList(): Array<LabelFilter.Expression>;
  setExpressionsList(value: Array<LabelFilter.Expression>): void;
  addExpressions(value?: LabelFilter.Expression, index?: number): LabelFilter.Expression;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): LabelFilter.AsObject;
  static toObject(includeInstance: boolean, msg: LabelFilter): LabelFilter.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: LabelFilter, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): LabelFilter;
  static deserializeBinaryFromReader(message: LabelFilter, reader: jspb.BinaryReader): LabelFilter;
}

export namespace LabelFilter {
  export type AsObject = {
    expressionsList: Array<LabelFilter.Expression.AsObject>,
  }

  export class Expression extends jspb.Message {
    getKey(): string;
    setKey(value: string): void;

    getOp(): LabelFilter.Expression.OperatorMap[keyof LabelFilter.Expression.OperatorMap];
    setOp(value: LabelFilter.Expression.OperatorMap[keyof LabelFilter.Expression.OperatorMap]): void;

    clearValuesList(): void;
    getValuesList(): Array<string>;
    setValuesList(value: Array<string>): void;
    addValues(value: string, index?: number): string;

    serializeBinary(): Uint8Array;
    toObject(includeInstance?: boolean): Expression.AsObject;
    static toObject(includeInstance: boolean, msg: Expression): Expression.AsObject;
    static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
    static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
    static serializeBinaryToWriter(message: Expression, writer: jspb.BinaryWriter): void;
    static deserializeBinary(bytes: Uint8Array): Expression;
    static deserializeBinaryFromReader(message: Expression, reader: jspb.BinaryReader): Expression;
  }

  export namespace Expression {
    export type AsObject = {
      key: string,
      op: LabelFilter.Expression.OperatorMap[keyof LabelFilter.Expression.OperatorMap],
      valuesList: Array<string>,
    }

    export interface OperatorMap {
      OP_IN: 0;
      OP_NOT_IN: 1;
      OP_EXISTS: 2;
      OP_NOT_EXISTS: 3;
    }

    export const Operator: OperatorMap;
  }
}

export class StreamAppsRequest extends jspb.Message {
  getPageSize(): number;
  setPageSize(value: number): void;

  getPageToken(): string;
  setPageToken(value: string): void;

  clearNameFiltersList(): void;
  getNameFiltersList(): Array<string>;
  setNameFiltersList(value: Array<string>): void;
  addNameFilters(value: string, index?: number): string;

  clearLabelFiltersList(): void;
  getLabelFiltersList(): Array<LabelFilter>;
  setLabelFiltersList(value: Array<LabelFilter>): void;
  addLabelFilters(value?: LabelFilter, index?: number): LabelFilter;

  getStreamUpdates(): boolean;
  setStreamUpdates(value: boolean): void;

  getStreamCreates(): boolean;
  setStreamCreates(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamAppsRequest.AsObject;
  static toObject(includeInstance: boolean, msg: StreamAppsRequest): StreamAppsRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamAppsRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamAppsRequest;
  static deserializeBinaryFromReader(message: StreamAppsRequest, reader: jspb.BinaryReader): StreamAppsRequest;
}

export namespace StreamAppsRequest {
  export type AsObject = {
    pageSize: number,
    pageToken: string,
    nameFiltersList: Array<string>,
    labelFiltersList: Array<LabelFilter.AsObject>,
    streamUpdates: boolean,
    streamCreates: boolean,
  }
}

export class StreamAppsResponse extends jspb.Message {
  clearAppsList(): void;
  getAppsList(): Array<App>;
  setAppsList(value: Array<App>): void;
  addApps(value?: App, index?: number): App;

  getPageComplete(): boolean;
  setPageComplete(value: boolean): void;

  getNextPageToken(): string;
  setNextPageToken(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamAppsResponse.AsObject;
  static toObject(includeInstance: boolean, msg: StreamAppsResponse): StreamAppsResponse.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamAppsResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamAppsResponse;
  static deserializeBinaryFromReader(message: StreamAppsResponse, reader: jspb.BinaryReader): StreamAppsResponse;
}

export namespace StreamAppsResponse {
  export type AsObject = {
    appsList: Array<App.AsObject>,
    pageComplete: boolean,
    nextPageToken: string,
  }
}

export class StreamReleasesRequest extends jspb.Message {
  getPageSize(): number;
  setPageSize(value: number): void;

  getPageToken(): string;
  setPageToken(value: string): void;

  clearNameFiltersList(): void;
  getNameFiltersList(): Array<string>;
  setNameFiltersList(value: Array<string>): void;
  addNameFilters(value: string, index?: number): string;

  clearLabelFiltersList(): void;
  getLabelFiltersList(): Array<LabelFilter>;
  setLabelFiltersList(value: Array<LabelFilter>): void;
  addLabelFilters(value?: LabelFilter, index?: number): LabelFilter;

  getStreamUpdates(): boolean;
  setStreamUpdates(value: boolean): void;

  getStreamCreates(): boolean;
  setStreamCreates(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamReleasesRequest.AsObject;
  static toObject(includeInstance: boolean, msg: StreamReleasesRequest): StreamReleasesRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamReleasesRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamReleasesRequest;
  static deserializeBinaryFromReader(message: StreamReleasesRequest, reader: jspb.BinaryReader): StreamReleasesRequest;
}

export namespace StreamReleasesRequest {
  export type AsObject = {
    pageSize: number,
    pageToken: string,
    nameFiltersList: Array<string>,
    labelFiltersList: Array<LabelFilter.AsObject>,
    streamUpdates: boolean,
    streamCreates: boolean,
  }
}

export class StreamReleasesResponse extends jspb.Message {
  clearReleasesList(): void;
  getReleasesList(): Array<Release>;
  setReleasesList(value: Array<Release>): void;
  addReleases(value?: Release, index?: number): Release;

  getPageComplete(): boolean;
  setPageComplete(value: boolean): void;

  getNextPageToken(): string;
  setNextPageToken(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamReleasesResponse.AsObject;
  static toObject(includeInstance: boolean, msg: StreamReleasesResponse): StreamReleasesResponse.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamReleasesResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamReleasesResponse;
  static deserializeBinaryFromReader(message: StreamReleasesResponse, reader: jspb.BinaryReader): StreamReleasesResponse;
}

export namespace StreamReleasesResponse {
  export type AsObject = {
    releasesList: Array<Release.AsObject>,
    pageComplete: boolean,
    nextPageToken: string,
  }
}

export class StreamScalesRequest extends jspb.Message {
  getPageSize(): number;
  setPageSize(value: number): void;

  getPageToken(): string;
  setPageToken(value: string): void;

  clearNameFiltersList(): void;
  getNameFiltersList(): Array<string>;
  setNameFiltersList(value: Array<string>): void;
  addNameFilters(value: string, index?: number): string;

  clearStateFiltersList(): void;
  getStateFiltersList(): Array<ScaleRequestStateMap[keyof ScaleRequestStateMap]>;
  setStateFiltersList(value: Array<ScaleRequestStateMap[keyof ScaleRequestStateMap]>): void;
  addStateFilters(value: ScaleRequestStateMap[keyof ScaleRequestStateMap], index?: number): ScaleRequestStateMap[keyof ScaleRequestStateMap];

  getStreamUpdates(): boolean;
  setStreamUpdates(value: boolean): void;

  getStreamCreates(): boolean;
  setStreamCreates(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamScalesRequest.AsObject;
  static toObject(includeInstance: boolean, msg: StreamScalesRequest): StreamScalesRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamScalesRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamScalesRequest;
  static deserializeBinaryFromReader(message: StreamScalesRequest, reader: jspb.BinaryReader): StreamScalesRequest;
}

export namespace StreamScalesRequest {
  export type AsObject = {
    pageSize: number,
    pageToken: string,
    nameFiltersList: Array<string>,
    stateFiltersList: Array<ScaleRequestStateMap[keyof ScaleRequestStateMap]>,
    streamUpdates: boolean,
    streamCreates: boolean,
  }
}

export class StreamScalesResponse extends jspb.Message {
  clearScaleRequestsList(): void;
  getScaleRequestsList(): Array<ScaleRequest>;
  setScaleRequestsList(value: Array<ScaleRequest>): void;
  addScaleRequests(value?: ScaleRequest, index?: number): ScaleRequest;

  getPageComplete(): boolean;
  setPageComplete(value: boolean): void;

  getNextPageToken(): string;
  setNextPageToken(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamScalesResponse.AsObject;
  static toObject(includeInstance: boolean, msg: StreamScalesResponse): StreamScalesResponse.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamScalesResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamScalesResponse;
  static deserializeBinaryFromReader(message: StreamScalesResponse, reader: jspb.BinaryReader): StreamScalesResponse;
}

export namespace StreamScalesResponse {
  export type AsObject = {
    scaleRequestsList: Array<ScaleRequest.AsObject>,
    pageComplete: boolean,
    nextPageToken: string,
  }
}

export class StreamDeploymentsRequest extends jspb.Message {
  getPageSize(): number;
  setPageSize(value: number): void;

  getPageToken(): string;
  setPageToken(value: string): void;

  clearNameFiltersList(): void;
  getNameFiltersList(): Array<string>;
  setNameFiltersList(value: Array<string>): void;
  addNameFilters(value: string, index?: number): string;

  clearTypeFiltersList(): void;
  getTypeFiltersList(): Array<ReleaseTypeMap[keyof ReleaseTypeMap]>;
  setTypeFiltersList(value: Array<ReleaseTypeMap[keyof ReleaseTypeMap]>): void;
  addTypeFilters(value: ReleaseTypeMap[keyof ReleaseTypeMap], index?: number): ReleaseTypeMap[keyof ReleaseTypeMap];

  clearStatusFiltersList(): void;
  getStatusFiltersList(): Array<DeploymentStatusMap[keyof DeploymentStatusMap]>;
  setStatusFiltersList(value: Array<DeploymentStatusMap[keyof DeploymentStatusMap]>): void;
  addStatusFilters(value: DeploymentStatusMap[keyof DeploymentStatusMap], index?: number): DeploymentStatusMap[keyof DeploymentStatusMap];

  getStreamUpdates(): boolean;
  setStreamUpdates(value: boolean): void;

  getStreamCreates(): boolean;
  setStreamCreates(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamDeploymentsRequest.AsObject;
  static toObject(includeInstance: boolean, msg: StreamDeploymentsRequest): StreamDeploymentsRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamDeploymentsRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamDeploymentsRequest;
  static deserializeBinaryFromReader(message: StreamDeploymentsRequest, reader: jspb.BinaryReader): StreamDeploymentsRequest;
}

export namespace StreamDeploymentsRequest {
  export type AsObject = {
    pageSize: number,
    pageToken: string,
    nameFiltersList: Array<string>,
    typeFiltersList: Array<ReleaseTypeMap[keyof ReleaseTypeMap]>,
    statusFiltersList: Array<DeploymentStatusMap[keyof DeploymentStatusMap]>,
    streamUpdates: boolean,
    streamCreates: boolean,
  }
}

export class StreamDeploymentsResponse extends jspb.Message {
  clearDeploymentsList(): void;
  getDeploymentsList(): Array<ExpandedDeployment>;
  setDeploymentsList(value: Array<ExpandedDeployment>): void;
  addDeployments(value?: ExpandedDeployment, index?: number): ExpandedDeployment;

  getPageComplete(): boolean;
  setPageComplete(value: boolean): void;

  getNextPageToken(): string;
  setNextPageToken(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): StreamDeploymentsResponse.AsObject;
  static toObject(includeInstance: boolean, msg: StreamDeploymentsResponse): StreamDeploymentsResponse.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: StreamDeploymentsResponse, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): StreamDeploymentsResponse;
  static deserializeBinaryFromReader(message: StreamDeploymentsResponse, reader: jspb.BinaryReader): StreamDeploymentsResponse;
}

export namespace StreamDeploymentsResponse {
  export type AsObject = {
    deploymentsList: Array<ExpandedDeployment.AsObject>,
    pageComplete: boolean,
    nextPageToken: string,
  }
}

export class UpdateAppRequest extends jspb.Message {
  hasApp(): boolean;
  clearApp(): void;
  getApp(): App | undefined;
  setApp(value?: App): void;

  hasUpdateMask(): boolean;
  clearUpdateMask(): void;
  getUpdateMask(): google_protobuf_field_mask_pb.FieldMask | undefined;
  setUpdateMask(value?: google_protobuf_field_mask_pb.FieldMask): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): UpdateAppRequest.AsObject;
  static toObject(includeInstance: boolean, msg: UpdateAppRequest): UpdateAppRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: UpdateAppRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): UpdateAppRequest;
  static deserializeBinaryFromReader(message: UpdateAppRequest, reader: jspb.BinaryReader): UpdateAppRequest;
}

export namespace UpdateAppRequest {
  export type AsObject = {
    app?: App.AsObject,
    updateMask?: google_protobuf_field_mask_pb.FieldMask.AsObject,
  }
}

export class CreateScaleRequest extends jspb.Message {
  getParent(): string;
  setParent(value: string): void;

  getProcessesMap(): jspb.Map<string, number>;
  clearProcessesMap(): void;
  getTagsMap(): jspb.Map<string, DeploymentProcessTags>;
  clearTagsMap(): void;
  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): CreateScaleRequest.AsObject;
  static toObject(includeInstance: boolean, msg: CreateScaleRequest): CreateScaleRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: CreateScaleRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): CreateScaleRequest;
  static deserializeBinaryFromReader(message: CreateScaleRequest, reader: jspb.BinaryReader): CreateScaleRequest;
}

export namespace CreateScaleRequest {
  export type AsObject = {
    parent: string,
    processesMap: Array<[string, number]>,
    tagsMap: Array<[string, DeploymentProcessTags.AsObject]>,
  }
}

export class CreateReleaseRequest extends jspb.Message {
  getParent(): string;
  setParent(value: string): void;

  hasRelease(): boolean;
  clearRelease(): void;
  getRelease(): Release | undefined;
  setRelease(value?: Release): void;

  getRequestId(): string;
  setRequestId(value: string): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): CreateReleaseRequest.AsObject;
  static toObject(includeInstance: boolean, msg: CreateReleaseRequest): CreateReleaseRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: CreateReleaseRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): CreateReleaseRequest;
  static deserializeBinaryFromReader(message: CreateReleaseRequest, reader: jspb.BinaryReader): CreateReleaseRequest;
}

export namespace CreateReleaseRequest {
  export type AsObject = {
    parent: string,
    release?: Release.AsObject,
    requestId: string,
  }
}

export class CreateDeploymentRequest extends jspb.Message {
  getParent(): string;
  setParent(value: string): void;

  hasScaleRequest(): boolean;
  clearScaleRequest(): void;
  getScaleRequest(): CreateScaleRequest | undefined;
  setScaleRequest(value?: CreateScaleRequest): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): CreateDeploymentRequest.AsObject;
  static toObject(includeInstance: boolean, msg: CreateDeploymentRequest): CreateDeploymentRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: CreateDeploymentRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): CreateDeploymentRequest;
  static deserializeBinaryFromReader(message: CreateDeploymentRequest, reader: jspb.BinaryReader): CreateDeploymentRequest;
}

export namespace CreateDeploymentRequest {
  export type AsObject = {
    parent: string,
    scaleRequest?: CreateScaleRequest.AsObject,
  }
}

export class App extends jspb.Message {
  getName(): string;
  setName(value: string): void;

  getDisplayName(): string;
  setDisplayName(value: string): void;

  getLabelsMap(): jspb.Map<string, string>;
  clearLabelsMap(): void;
  getDeployTimeout(): number;
  setDeployTimeout(value: number): void;

  getStrategy(): string;
  setStrategy(value: string): void;

  getRelease(): string;
  setRelease(value: string): void;

  hasCreateTime(): boolean;
  clearCreateTime(): void;
  getCreateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setCreateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasUpdateTime(): boolean;
  clearUpdateTime(): void;
  getUpdateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setUpdateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasDeleteTime(): boolean;
  clearDeleteTime(): void;
  getDeleteTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setDeleteTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): App.AsObject;
  static toObject(includeInstance: boolean, msg: App): App.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: App, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): App;
  static deserializeBinaryFromReader(message: App, reader: jspb.BinaryReader): App;
}

export namespace App {
  export type AsObject = {
    name: string,
    displayName: string,
    labelsMap: Array<[string, string]>,
    deployTimeout: number,
    strategy: string,
    release: string,
    createTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    updateTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    deleteTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
  }
}

export class HostHealthCheck extends jspb.Message {
  getType(): string;
  setType(value: string): void;

  hasInterval(): boolean;
  clearInterval(): void;
  getInterval(): google_protobuf_duration_pb.Duration | undefined;
  setInterval(value?: google_protobuf_duration_pb.Duration): void;

  getThreshold(): number;
  setThreshold(value: number): void;

  getKillDown(): boolean;
  setKillDown(value: boolean): void;

  hasStartTimeout(): boolean;
  clearStartTimeout(): void;
  getStartTimeout(): google_protobuf_duration_pb.Duration | undefined;
  setStartTimeout(value?: google_protobuf_duration_pb.Duration): void;

  getPath(): string;
  setPath(value: string): void;

  getHost(): string;
  setHost(value: string): void;

  getMatch(): string;
  setMatch(value: string): void;

  getStatus(): number;
  setStatus(value: number): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): HostHealthCheck.AsObject;
  static toObject(includeInstance: boolean, msg: HostHealthCheck): HostHealthCheck.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: HostHealthCheck, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): HostHealthCheck;
  static deserializeBinaryFromReader(message: HostHealthCheck, reader: jspb.BinaryReader): HostHealthCheck;
}

export namespace HostHealthCheck {
  export type AsObject = {
    type: string,
    interval?: google_protobuf_duration_pb.Duration.AsObject,
    threshold: number,
    killDown: boolean,
    startTimeout?: google_protobuf_duration_pb.Duration.AsObject,
    path: string,
    host: string,
    match: string,
    status: number,
  }
}

export class HostService extends jspb.Message {
  getDisplayName(): string;
  setDisplayName(value: string): void;

  getCreate(): boolean;
  setCreate(value: boolean): void;

  hasCheck(): boolean;
  clearCheck(): void;
  getCheck(): HostHealthCheck | undefined;
  setCheck(value?: HostHealthCheck): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): HostService.AsObject;
  static toObject(includeInstance: boolean, msg: HostService): HostService.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: HostService, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): HostService;
  static deserializeBinaryFromReader(message: HostService, reader: jspb.BinaryReader): HostService;
}

export namespace HostService {
  export type AsObject = {
    displayName: string,
    create: boolean,
    check?: HostHealthCheck.AsObject,
  }
}

export class Port extends jspb.Message {
  getPort(): number;
  setPort(value: number): void;

  getProto(): string;
  setProto(value: string): void;

  hasService(): boolean;
  clearService(): void;
  getService(): HostService | undefined;
  setService(value?: HostService): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): Port.AsObject;
  static toObject(includeInstance: boolean, msg: Port): Port.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: Port, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): Port;
  static deserializeBinaryFromReader(message: Port, reader: jspb.BinaryReader): Port;
}

export namespace Port {
  export type AsObject = {
    port: number,
    proto: string,
    service?: HostService.AsObject,
  }
}

export class VolumeReq extends jspb.Message {
  getPath(): string;
  setPath(value: string): void;

  getDeleteOnStop(): boolean;
  setDeleteOnStop(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): VolumeReq.AsObject;
  static toObject(includeInstance: boolean, msg: VolumeReq): VolumeReq.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: VolumeReq, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): VolumeReq;
  static deserializeBinaryFromReader(message: VolumeReq, reader: jspb.BinaryReader): VolumeReq;
}

export namespace VolumeReq {
  export type AsObject = {
    path: string,
    deleteOnStop: boolean,
  }
}

export class HostResourceSpec extends jspb.Message {
  getRequest(): number;
  setRequest(value: number): void;

  getLimit(): number;
  setLimit(value: number): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): HostResourceSpec.AsObject;
  static toObject(includeInstance: boolean, msg: HostResourceSpec): HostResourceSpec.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: HostResourceSpec, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): HostResourceSpec;
  static deserializeBinaryFromReader(message: HostResourceSpec, reader: jspb.BinaryReader): HostResourceSpec;
}

export namespace HostResourceSpec {
  export type AsObject = {
    request: number,
    limit: number,
  }
}

export class HostMount extends jspb.Message {
  getLocation(): string;
  setLocation(value: string): void;

  getTarget(): string;
  setTarget(value: string): void;

  getWriteable(): boolean;
  setWriteable(value: boolean): void;

  getDevice(): string;
  setDevice(value: string): void;

  getData(): string;
  setData(value: string): void;

  getFlags(): number;
  setFlags(value: number): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): HostMount.AsObject;
  static toObject(includeInstance: boolean, msg: HostMount): HostMount.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: HostMount, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): HostMount;
  static deserializeBinaryFromReader(message: HostMount, reader: jspb.BinaryReader): HostMount;
}

export namespace HostMount {
  export type AsObject = {
    location: string,
    target: string,
    writeable: boolean,
    device: string,
    data: string,
    flags: number,
  }
}

export class LibContainerDevice extends jspb.Message {
  getType(): number;
  setType(value: number): void;

  getPath(): string;
  setPath(value: string): void;

  getMajor(): number;
  setMajor(value: number): void;

  getMinor(): number;
  setMinor(value: number): void;

  getPermissions(): string;
  setPermissions(value: string): void;

  getFileMode(): number;
  setFileMode(value: number): void;

  getUid(): number;
  setUid(value: number): void;

  getGid(): number;
  setGid(value: number): void;

  getAllow(): boolean;
  setAllow(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): LibContainerDevice.AsObject;
  static toObject(includeInstance: boolean, msg: LibContainerDevice): LibContainerDevice.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: LibContainerDevice, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): LibContainerDevice;
  static deserializeBinaryFromReader(message: LibContainerDevice, reader: jspb.BinaryReader): LibContainerDevice;
}

export namespace LibContainerDevice {
  export type AsObject = {
    type: number,
    path: string,
    major: number,
    minor: number,
    permissions: string,
    fileMode: number,
    uid: number,
    gid: number,
    allow: boolean,
  }
}

export class ProcessType extends jspb.Message {
  clearArgsList(): void;
  getArgsList(): Array<string>;
  setArgsList(value: Array<string>): void;
  addArgs(value: string, index?: number): string;

  getEnvMap(): jspb.Map<string, string>;
  clearEnvMap(): void;
  clearPortsList(): void;
  getPortsList(): Array<Port>;
  setPortsList(value: Array<Port>): void;
  addPorts(value?: Port, index?: number): Port;

  clearVolumesList(): void;
  getVolumesList(): Array<VolumeReq>;
  setVolumesList(value: Array<VolumeReq>): void;
  addVolumes(value?: VolumeReq, index?: number): VolumeReq;

  getOmni(): boolean;
  setOmni(value: boolean): void;

  getHostNetwork(): boolean;
  setHostNetwork(value: boolean): void;

  getHostPidNamespace(): boolean;
  setHostPidNamespace(value: boolean): void;

  getService(): string;
  setService(value: string): void;

  getResurrect(): boolean;
  setResurrect(value: boolean): void;

  getResourcesMap(): jspb.Map<string, HostResourceSpec>;
  clearResourcesMap(): void;
  clearMountsList(): void;
  getMountsList(): Array<HostMount>;
  setMountsList(value: Array<HostMount>): void;
  addMounts(value?: HostMount, index?: number): HostMount;

  clearLinuxCapabilitiesList(): void;
  getLinuxCapabilitiesList(): Array<string>;
  setLinuxCapabilitiesList(value: Array<string>): void;
  addLinuxCapabilities(value: string, index?: number): string;

  clearAllowedDevicesList(): void;
  getAllowedDevicesList(): Array<LibContainerDevice>;
  setAllowedDevicesList(value: Array<LibContainerDevice>): void;
  addAllowedDevices(value?: LibContainerDevice, index?: number): LibContainerDevice;

  getWriteableCgroups(): boolean;
  setWriteableCgroups(value: boolean): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): ProcessType.AsObject;
  static toObject(includeInstance: boolean, msg: ProcessType): ProcessType.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: ProcessType, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): ProcessType;
  static deserializeBinaryFromReader(message: ProcessType, reader: jspb.BinaryReader): ProcessType;
}

export namespace ProcessType {
  export type AsObject = {
    argsList: Array<string>,
    envMap: Array<[string, string]>,
    portsList: Array<Port.AsObject>,
    volumesList: Array<VolumeReq.AsObject>,
    omni: boolean,
    hostNetwork: boolean,
    hostPidNamespace: boolean,
    service: string,
    resurrect: boolean,
    resourcesMap: Array<[string, HostResourceSpec.AsObject]>,
    mountsList: Array<HostMount.AsObject>,
    linuxCapabilitiesList: Array<string>,
    allowedDevicesList: Array<LibContainerDevice.AsObject>,
    writeableCgroups: boolean,
  }
}

export class Release extends jspb.Message {
  getName(): string;
  setName(value: string): void;

  clearArtifactsList(): void;
  getArtifactsList(): Array<string>;
  setArtifactsList(value: Array<string>): void;
  addArtifacts(value: string, index?: number): string;

  getEnvMap(): jspb.Map<string, string>;
  clearEnvMap(): void;
  getLabelsMap(): jspb.Map<string, string>;
  clearLabelsMap(): void;
  getProcessesMap(): jspb.Map<string, ProcessType>;
  clearProcessesMap(): void;
  getType(): ReleaseTypeMap[keyof ReleaseTypeMap];
  setType(value: ReleaseTypeMap[keyof ReleaseTypeMap]): void;

  hasCreateTime(): boolean;
  clearCreateTime(): void;
  getCreateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setCreateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasDeleteTime(): boolean;
  clearDeleteTime(): void;
  getDeleteTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setDeleteTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): Release.AsObject;
  static toObject(includeInstance: boolean, msg: Release): Release.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: Release, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): Release;
  static deserializeBinaryFromReader(message: Release, reader: jspb.BinaryReader): Release;
}

export namespace Release {
  export type AsObject = {
    name: string,
    artifactsList: Array<string>,
    envMap: Array<[string, string]>,
    labelsMap: Array<[string, string]>,
    processesMap: Array<[string, ProcessType.AsObject]>,
    type: ReleaseTypeMap[keyof ReleaseTypeMap],
    createTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    deleteTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
  }
}

export class ScaleRequest extends jspb.Message {
  getParent(): string;
  setParent(value: string): void;

  getName(): string;
  setName(value: string): void;

  getState(): ScaleRequestStateMap[keyof ScaleRequestStateMap];
  setState(value: ScaleRequestStateMap[keyof ScaleRequestStateMap]): void;

  getOldProcessesMap(): jspb.Map<string, number>;
  clearOldProcessesMap(): void;
  getNewProcessesMap(): jspb.Map<string, number>;
  clearNewProcessesMap(): void;
  getOldTagsMap(): jspb.Map<string, DeploymentProcessTags>;
  clearOldTagsMap(): void;
  getNewTagsMap(): jspb.Map<string, DeploymentProcessTags>;
  clearNewTagsMap(): void;
  hasCreateTime(): boolean;
  clearCreateTime(): void;
  getCreateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setCreateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasUpdateTime(): boolean;
  clearUpdateTime(): void;
  getUpdateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setUpdateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): ScaleRequest.AsObject;
  static toObject(includeInstance: boolean, msg: ScaleRequest): ScaleRequest.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: ScaleRequest, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): ScaleRequest;
  static deserializeBinaryFromReader(message: ScaleRequest, reader: jspb.BinaryReader): ScaleRequest;
}

export namespace ScaleRequest {
  export type AsObject = {
    parent: string,
    name: string,
    state: ScaleRequestStateMap[keyof ScaleRequestStateMap],
    oldProcessesMap: Array<[string, number]>,
    newProcessesMap: Array<[string, number]>,
    oldTagsMap: Array<[string, DeploymentProcessTags.AsObject]>,
    newTagsMap: Array<[string, DeploymentProcessTags.AsObject]>,
    createTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    updateTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
  }
}

export class DeploymentProcessTags extends jspb.Message {
  getTagsMap(): jspb.Map<string, string>;
  clearTagsMap(): void;
  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): DeploymentProcessTags.AsObject;
  static toObject(includeInstance: boolean, msg: DeploymentProcessTags): DeploymentProcessTags.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: DeploymentProcessTags, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): DeploymentProcessTags;
  static deserializeBinaryFromReader(message: DeploymentProcessTags, reader: jspb.BinaryReader): DeploymentProcessTags;
}

export namespace DeploymentProcessTags {
  export type AsObject = {
    tagsMap: Array<[string, string]>,
  }
}

export class ExpandedDeployment extends jspb.Message {
  getName(): string;
  setName(value: string): void;

  hasOldRelease(): boolean;
  clearOldRelease(): void;
  getOldRelease(): Release | undefined;
  setOldRelease(value?: Release): void;

  hasNewRelease(): boolean;
  clearNewRelease(): void;
  getNewRelease(): Release | undefined;
  setNewRelease(value?: Release): void;

  getType(): ReleaseTypeMap[keyof ReleaseTypeMap];
  setType(value: ReleaseTypeMap[keyof ReleaseTypeMap]): void;

  getStrategy(): string;
  setStrategy(value: string): void;

  getStatus(): DeploymentStatusMap[keyof DeploymentStatusMap];
  setStatus(value: DeploymentStatusMap[keyof DeploymentStatusMap]): void;

  getProcessesMap(): jspb.Map<string, number>;
  clearProcessesMap(): void;
  getTagsMap(): jspb.Map<string, DeploymentProcessTags>;
  clearTagsMap(): void;
  getDeployTimeout(): number;
  setDeployTimeout(value: number): void;

  hasCreateTime(): boolean;
  clearCreateTime(): void;
  getCreateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setCreateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasExpireTime(): boolean;
  clearExpireTime(): void;
  getExpireTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setExpireTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  hasEndTime(): boolean;
  clearEndTime(): void;
  getEndTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setEndTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): ExpandedDeployment.AsObject;
  static toObject(includeInstance: boolean, msg: ExpandedDeployment): ExpandedDeployment.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: ExpandedDeployment, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): ExpandedDeployment;
  static deserializeBinaryFromReader(message: ExpandedDeployment, reader: jspb.BinaryReader): ExpandedDeployment;
}

export namespace ExpandedDeployment {
  export type AsObject = {
    name: string,
    oldRelease?: Release.AsObject,
    newRelease?: Release.AsObject,
    type: ReleaseTypeMap[keyof ReleaseTypeMap],
    strategy: string,
    status: DeploymentStatusMap[keyof DeploymentStatusMap],
    processesMap: Array<[string, number]>,
    tagsMap: Array<[string, DeploymentProcessTags.AsObject]>,
    deployTimeout: number,
    createTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    expireTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
    endTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
  }
}

export class DeploymentEvent extends jspb.Message {
  getParent(): string;
  setParent(value: string): void;

  getJobType(): string;
  setJobType(value: string): void;

  getJobState(): DeploymentEvent.JobStateMap[keyof DeploymentEvent.JobStateMap];
  setJobState(value: DeploymentEvent.JobStateMap[keyof DeploymentEvent.JobStateMap]): void;

  getError(): string;
  setError(value: string): void;

  hasCreateTime(): boolean;
  clearCreateTime(): void;
  getCreateTime(): google_protobuf_timestamp_pb.Timestamp | undefined;
  setCreateTime(value?: google_protobuf_timestamp_pb.Timestamp): void;

  serializeBinary(): Uint8Array;
  toObject(includeInstance?: boolean): DeploymentEvent.AsObject;
  static toObject(includeInstance: boolean, msg: DeploymentEvent): DeploymentEvent.AsObject;
  static extensions: {[key: number]: jspb.ExtensionFieldInfo<jspb.Message>};
  static extensionsBinary: {[key: number]: jspb.ExtensionFieldBinaryInfo<jspb.Message>};
  static serializeBinaryToWriter(message: DeploymentEvent, writer: jspb.BinaryWriter): void;
  static deserializeBinary(bytes: Uint8Array): DeploymentEvent;
  static deserializeBinaryFromReader(message: DeploymentEvent, reader: jspb.BinaryReader): DeploymentEvent;
}

export namespace DeploymentEvent {
  export type AsObject = {
    parent: string,
    jobType: string,
    jobState: DeploymentEvent.JobStateMap[keyof DeploymentEvent.JobStateMap],
    error: string,
    createTime?: google_protobuf_timestamp_pb.Timestamp.AsObject,
  }

  export interface JobStateMap {
    PENDING: 0;
    BLOCKED: 1;
    STARTING: 2;
    UP: 3;
    STOPPING: 5;
    DOWN: 6;
    CRASHED: 7;
    FAILED: 8;
  }

  export const JobState: JobStateMap;
}

export interface ReleaseTypeMap {
  ANY: 0;
  CODE: 1;
  CONFIG: 2;
}

export const ReleaseType: ReleaseTypeMap;

export interface ScaleRequestStateMap {
  SCALE_PENDING: 0;
  SCALE_CANCELLED: 1;
  SCALE_COMPLETE: 2;
}

export const ScaleRequestState: ScaleRequestStateMap;

export interface DeploymentStatusMap {
  PENDING: 0;
  FAILED: 1;
  RUNNING: 2;
  COMPLETE: 3;
}

export const DeploymentStatus: DeploymentStatusMap;

