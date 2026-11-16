# Dashboard API spec

The OpenAPI 3.1 document of the dashboard API. The server code in `internal/api/gen` is generated from it.

## Layout

- `openapi.yaml`: info, servers, security, the security schemes, and every path by `$ref`. It lists no other components.
- `paths/<resource>.yaml`: one file per resource, one key per path, such as `paths/auth.yaml#/login`.
- `components/schemas/<resource>.yaml`, `components/schemas/errors.yaml`, `components/responses.yaml`.
- `openapi.gen.yaml`: the bundle, committed. Do not edit it.

## Rules

- Every operation has an `operationId` in lowerCamelCase, a summary, a description and a tag.
- Every operation declares its permission in `x-runcoil-permission`, as `resource:verb`, `self` when any signed-in user may call it on their own account, or `authenticated` when any signed-in user may call it, such as looking up users. Only `login` and `logout` have `security: []` and no permission: logout clears the cookie even when the session has ended.
- Every error body is an `ErrorResponse`: a `code` string, a `message`, and for `invalid_request` the `params` at fault.
- Every operation lists its success responses and the 4xx errors it returns, each as a `$ref` to the shared response of its code in `components/responses.yaml` (`InvalidRequest`, `Unauthenticated`, `PermissionDenied`, `NotFound`, `Conflict`, `PayloadTooLarge`), which holds the `ErrorResponse` schema and an example, with a `description` beside it that names the code and when this operation returns it, in double quotes, for example `description: "unauthenticated: no live session"`. A 500 is not listed: any operation may return it.
- A request body schema holds no `$ref`: it declares every property, enums included, inline. The validator reports every field at fault only for a body schema it can read on its own; with a `$ref` it stops at the first one.
- Every object has `additionalProperties: false` and lists `required` after `properties`. Every property has a description, an example and an `x-order`.
- Lists are written as `-` items, never as `[...]`, except the empty `security: []`.
- A blank line separates the top-level keys of a file: each schema, response and path.
- A path key is a valid component name: letters, digits, `.`, `_` and `-`.

## Generate

```sh
task oapi-gen-server
```

It bundles the spec, lints the bundle with vacuum, and generates the server with oapi-codegen.
