// package: flynn.api.v1
// file: controller.proto

import * as controller_pb from "./controller_pb";
import * as google_protobuf_empty_pb from "google-protobuf/google/protobuf/empty_pb";
import {grpc} from "@improbable-eng/grpc-web";

type ControllerStatus = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: false;
  readonly requestType: typeof google_protobuf_empty_pb.Empty;
  readonly responseType: typeof controller_pb.StatusResponse;
};

type ControllerStreamApps = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: true;
  readonly requestType: typeof controller_pb.StreamAppsRequest;
  readonly responseType: typeof controller_pb.StreamAppsResponse;
};

type ControllerStreamReleases = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: true;
  readonly requestType: typeof controller_pb.StreamReleasesRequest;
  readonly responseType: typeof controller_pb.StreamReleasesResponse;
};

type ControllerStreamScales = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: true;
  readonly requestType: typeof controller_pb.StreamScalesRequest;
  readonly responseType: typeof controller_pb.StreamScalesResponse;
};

type ControllerStreamDeployments = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: true;
  readonly requestType: typeof controller_pb.StreamDeploymentsRequest;
  readonly responseType: typeof controller_pb.StreamDeploymentsResponse;
};

type ControllerUpdateApp = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: false;
  readonly requestType: typeof controller_pb.UpdateAppRequest;
  readonly responseType: typeof controller_pb.App;
};

type ControllerCreateScale = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: false;
  readonly requestType: typeof controller_pb.CreateScaleRequest;
  readonly responseType: typeof controller_pb.ScaleRequest;
};

type ControllerCreateRelease = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: false;
  readonly requestType: typeof controller_pb.CreateReleaseRequest;
  readonly responseType: typeof controller_pb.Release;
};

type ControllerCreateDeployment = {
  readonly methodName: string;
  readonly service: typeof Controller;
  readonly requestStream: false;
  readonly responseStream: true;
  readonly requestType: typeof controller_pb.CreateDeploymentRequest;
  readonly responseType: typeof controller_pb.DeploymentEvent;
};

export class Controller {
  static readonly serviceName: string;
  static readonly Status: ControllerStatus;
  static readonly StreamApps: ControllerStreamApps;
  static readonly StreamReleases: ControllerStreamReleases;
  static readonly StreamScales: ControllerStreamScales;
  static readonly StreamDeployments: ControllerStreamDeployments;
  static readonly UpdateApp: ControllerUpdateApp;
  static readonly CreateScale: ControllerCreateScale;
  static readonly CreateRelease: ControllerCreateRelease;
  static readonly CreateDeployment: ControllerCreateDeployment;
}

export type ServiceError = { message: string, code: number; metadata: grpc.Metadata }
export type Status = { details: string, code: number; metadata: grpc.Metadata }

interface UnaryResponse {
  cancel(): void;
}
interface ResponseStream<T> {
  cancel(): void;
  on(type: 'data', handler: (message: T) => void): ResponseStream<T>;
  on(type: 'end', handler: (status?: Status) => void): ResponseStream<T>;
  on(type: 'status', handler: (status: Status) => void): ResponseStream<T>;
}
interface RequestStream<T> {
  write(message: T): RequestStream<T>;
  end(): void;
  cancel(): void;
  on(type: 'end', handler: (status?: Status) => void): RequestStream<T>;
  on(type: 'status', handler: (status: Status) => void): RequestStream<T>;
}
interface BidirectionalStream<ReqT, ResT> {
  write(message: ReqT): BidirectionalStream<ReqT, ResT>;
  end(): void;
  cancel(): void;
  on(type: 'data', handler: (message: ResT) => void): BidirectionalStream<ReqT, ResT>;
  on(type: 'end', handler: (status?: Status) => void): BidirectionalStream<ReqT, ResT>;
  on(type: 'status', handler: (status: Status) => void): BidirectionalStream<ReqT, ResT>;
}

export class ControllerClient {
  readonly serviceHost: string;

  constructor(serviceHost: string, options?: grpc.RpcOptions);
  status(
    requestMessage: google_protobuf_empty_pb.Empty,
    metadata: grpc.Metadata,
    callback: (error: ServiceError|null, responseMessage: controller_pb.StatusResponse|null) => void
  ): UnaryResponse;
  status(
    requestMessage: google_protobuf_empty_pb.Empty,
    callback: (error: ServiceError|null, responseMessage: controller_pb.StatusResponse|null) => void
  ): UnaryResponse;
  streamApps(requestMessage: controller_pb.StreamAppsRequest, metadata?: grpc.Metadata): ResponseStream<controller_pb.StreamAppsResponse>;
  streamReleases(requestMessage: controller_pb.StreamReleasesRequest, metadata?: grpc.Metadata): ResponseStream<controller_pb.StreamReleasesResponse>;
  streamScales(requestMessage: controller_pb.StreamScalesRequest, metadata?: grpc.Metadata): ResponseStream<controller_pb.StreamScalesResponse>;
  streamDeployments(requestMessage: controller_pb.StreamDeploymentsRequest, metadata?: grpc.Metadata): ResponseStream<controller_pb.StreamDeploymentsResponse>;
  updateApp(
    requestMessage: controller_pb.UpdateAppRequest,
    metadata: grpc.Metadata,
    callback: (error: ServiceError|null, responseMessage: controller_pb.App|null) => void
  ): UnaryResponse;
  updateApp(
    requestMessage: controller_pb.UpdateAppRequest,
    callback: (error: ServiceError|null, responseMessage: controller_pb.App|null) => void
  ): UnaryResponse;
  createScale(
    requestMessage: controller_pb.CreateScaleRequest,
    metadata: grpc.Metadata,
    callback: (error: ServiceError|null, responseMessage: controller_pb.ScaleRequest|null) => void
  ): UnaryResponse;
  createScale(
    requestMessage: controller_pb.CreateScaleRequest,
    callback: (error: ServiceError|null, responseMessage: controller_pb.ScaleRequest|null) => void
  ): UnaryResponse;
  createRelease(
    requestMessage: controller_pb.CreateReleaseRequest,
    metadata: grpc.Metadata,
    callback: (error: ServiceError|null, responseMessage: controller_pb.Release|null) => void
  ): UnaryResponse;
  createRelease(
    requestMessage: controller_pb.CreateReleaseRequest,
    callback: (error: ServiceError|null, responseMessage: controller_pb.Release|null) => void
  ): UnaryResponse;
  createDeployment(requestMessage: controller_pb.CreateDeploymentRequest, metadata?: grpc.Metadata): ResponseStream<controller_pb.DeploymentEvent>;
}

