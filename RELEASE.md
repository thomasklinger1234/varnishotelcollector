# Release Procedure

Collector build and testing is currently not automated. This means that there are still certain operations that need to be performed manually in order to make a release.

## Release workflow

1. Determine the version number that will be assigned to the release. Usually, we increment the minor version number and set the patch number to 0.
   In this document, we are using `v0.11.0` as the version to be released, following `v0.10.0`. 
2. Create a new git tag from the last commit to be released
    - If the release needs changes prior to release, perform those changes and commit them
    - `git tag v0.11.0`
3. Push the git tag with `git push --tags`
4. Create a new GitHub release with name `v0.11.0`
5. Push artifacts with `make distribution-release VERSION=0.11.0`