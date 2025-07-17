# Requirements Document

## Introduction

This feature aims to fix the Docker image pulling issue in the sketch tool. Currently, when users run the sketch binary, it attempts to pull a Docker image with a specific tag based on a hash of the Dockerfile content. However, this image may not exist in the registry, causing the tool to fail with an error message. This feature will implement a more robust Docker image handling mechanism to ensure users can always run the sketch tool successfully.

## Requirements

### Requirement 1

**User Story:** As a sketch user, I want the tool to use a reliable Docker image source, so that I can run the tool without encountering "manifest unknown" errors.

#### Acceptance Criteria

1. WHEN the sketch tool is launched THEN the system SHALL attempt to pull the Docker image using a reliable tag.
2. WHEN the specific hash-based Docker image is not found THEN the system SHALL fall back to a stable tag (like "latest" or a specific version).
3. WHEN no Docker image can be pulled THEN the system SHALL provide a clear error message with troubleshooting steps.

### Requirement 2

**User Story:** As a sketch developer, I want a reliable mechanism for publishing and versioning Docker images, so that users always have access to compatible images.

#### Acceptance Criteria

1. WHEN a new version of sketch is released THEN the system SHALL ensure a compatible Docker image is published.
2. WHEN the Dockerfile content changes THEN the system SHALL generate a new hash and publish a new image with that hash tag.
3. WHEN publishing Docker images THEN the system SHALL also tag the image with the sketch version for better traceability.

### Requirement 3

**User Story:** As a sketch user, I want to be able to specify a custom Docker image, so that I can use my own modified version if needed.

#### Acceptance Criteria

1. WHEN the user provides a custom Docker image via the `-base-image` flag THEN the system SHALL use that image instead of the default one.
2. WHEN using a custom Docker image THEN the system SHALL validate that it contains the necessary components for sketch to function properly.
3. WHEN a custom Docker image validation fails THEN the system SHALL provide a clear error message explaining what's missing.

### Requirement 4

**User Story:** As a sketch user, I want the tool to handle Docker image pulling gracefully, so that I don't experience unexpected failures.

#### Acceptance Criteria

1. WHEN network connectivity is limited THEN the system SHALL attempt to use a locally cached Docker image if available.
2. WHEN Docker is not installed or running THEN the system SHALL provide a clear error message with installation instructions.
3. WHEN Docker image pulling fails due to rate limiting or authentication issues THEN the system SHALL provide specific troubleshooting guidance.