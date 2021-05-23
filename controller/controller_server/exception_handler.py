from decorator import decorator

import grpc

import controller.array_action.errors as array_errors
from controller.common.csi_logger import get_stdout_logger
from controller.controller_server.errors import ObjectIdError, ValidationException

logger = get_stdout_logger()

status_codes_by_exception = {
    ValidationException: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.InvalidArgumentError: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.PoolDoesNotExist: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.PoolDoesNotMatchCapabilities: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.SpaceEfficiencyNotSupported: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.IllegalObjectName: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.IllegalObjectID: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.PoolParameterIsMissing: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.UnsupportedConnectivityTypeError: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.SnapshotSourcePoolMismatch: grpc.StatusCode.INVALID_ARGUMENT,
    array_errors.ObjectNotFoundError: grpc.StatusCode.NOT_FOUND,
    array_errors.HostNotFoundError: grpc.StatusCode.NOT_FOUND,
    array_errors.NoIscsiTargetsFoundError: grpc.StatusCode.NOT_FOUND,
    array_errors.VolumeAlreadyUnmappedError: grpc.StatusCode.NOT_FOUND,
    ObjectIdError: grpc.StatusCode.NOT_FOUND,
    array_errors.PermissionDeniedError: grpc.StatusCode.PERMISSION_DENIED,
    array_errors.ObjectIsStillInUseError: grpc.StatusCode.FAILED_PRECONDITION,
    array_errors.VolumeMappedToMultipleHostsError: grpc.StatusCode.FAILED_PRECONDITION,
    array_errors.LunAlreadyInUseError: grpc.StatusCode.RESOURCE_EXHAUSTED,
    array_errors.NoAvailableLunError: grpc.StatusCode.RESOURCE_EXHAUSTED,
    array_errors.NotEnoughSpaceInPool: grpc.StatusCode.RESOURCE_EXHAUSTED,
    array_errors.SnapshotAlreadyExists: grpc.StatusCode.ALREADY_EXISTS,
    array_errors.VolumeAlreadyExists: grpc.StatusCode.ALREADY_EXISTS
}


def handle_exception(ex, context, status_code, response_type):
    logger.exception(ex)
    context.set_details(str(ex))
    context.set_code(status_code)
    return response_type()


def handle_common_exceptions(response_type):
    @decorator
    def handle_common_exceptions_with_response(controller_method, servicer, request, context):
        try:
            return controller_method(servicer, request, context)
        except Exception as ex:
            status_code = status_codes_by_exception.get(type(ex), grpc.StatusCode.INTERNAL)
            return handle_exception(ex, context, status_code, response_type)

    return handle_common_exceptions_with_response
