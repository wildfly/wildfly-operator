# Contributing To WildFly Operator

Firstly, anyone is more than welcome to contribute to WildFly Operator, so big thanks for considering it. You can contribute by reporting and fixing bugs, improving the documentation or requesting and implementing new features.  

You only must follow the guidelines described on this document.

## Legal

All contributions to this repository are licensed under the [Apache License](https://www.apache.org/licenses/LICENSE-2.0), version 2.0 or later, or, if another license is specified as governing the file or directory being modified, such other license.

All contributions are subject to the [Developer Certificate of Origin (DCO)](https://developercertificate.org/).
The DCO text is also included verbatim in the [dco.txt](dco.txt) file in the root directory of the repository.

### Compliance with Laws and Regulations

All contributions must comply with applicable laws and regulations, including U.S. export control and sanctions restrictions.
For background, see the Linux Foundation’s guidance:
[Navigating Global Regulations and Open Source: US OFAC Sanctions](https://www.linuxfoundation.org/blog/navigating-global-regulations-and-open-source-us-ofac-sanctions).

## Code of Conduct

We are serious about making this a welcoming, happy project. We will not tolerate discrimination,
aggressive or insulting behaviour.

To this end, the project and everyone participating in it is bound by the [Code of
Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report
unacceptable behaviour to any of the project admins.

## Bugs

If you find a bug, please [open an issue](https://github.com/wildfly/wildfly-operator/issues)! Do try
to include all the details needed to recreate your problem. This is likely to include:

- The version of the WildFly Operator
- The exact platform and version of the platform that you're running on
- The steps taken to cause the bug

## Building Features and Documentation

If you're looking for something to work on, take look at the issue tracker, in particular any items
labelled [good first issue](https://github.com/wildfly/wildfly-operator/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).
Please leave a comment on the issue to mention that you have started work, in order to avoid
multiple people working on the same issue.

If you have an idea for a feature - whether you have time to work on it - please also open an
issue describing your feature and label it "enhancement". We can then discuss it as a community and
see what can be done. Please be aware that some features may not align with the project goals and
might therefore be closed. In particular, please don't start work on a new feature without
discussing it first to avoid wasting effort. We do commit to listening to all proposals and will do
our best to work something out!

## Pull Request Process

On opening a PR, a GitHub action will execute the test suite against the new code. All code is
required to pass the tests, and new code must be accompanied by new tests. 

All PRs have to be reviewed and signed off by another developer before being merged to the main
branch. This review will likely ask for some changes to the code - please don't be alarmed or upset
at this; it is expected that all PRs will need tweaks and a normal part of the process.

Be aware that all WildFly Operator code is released under the [Apache 2.0 licence](LICENSE).

## Development environment setup

The WildFly Operator [README](README.adoc) contains a section that describes how you can work with the WildFly Operator from the developer point of view.
Take a look at the [Developer Instructions](https://github.com/wildfly/wildfly-operator/blob/main/README.adoc#developer-instructions) section to learn how you can use the WildFly Operator on your local environment for development purposes.

## Thanks

This guideline has been elaborated following these resources:
[Java Operator SDK](https://github.com/java-operator-sdk/java-operator-sdk/blob/main/CONTRIBUTING.md)