---
title: How To Deploy Java
layout: docs
---

# How to Deploy Java

Flynn supports deploying Java applications which use either the [Maven](http://maven.apache.org/)
or [Gradle](http://www.gradle.org/) build tools, and supports [Grails](https://grails.org/)
applications through the [Java](https://github.com/heroku/heroku-buildpack-java), [Gradle](https://github.com/heroku/heroku-buildpack-gradle), and [Grails](https://github.com/heroku/heroku-buildpack-grails) buildpacks.

Flynn uses [OpenJDK](http://openjdk.java.net) to run Java applications.

## Detection

Flynn detects Java applications using the following rules:

* A `pom.xml` file in the root directory indicates a Java application which uses
the Maven build tool.
* A `gradlew`, `build.gradle`, or `settings.gradle` file in the root
directory indicates a Java application which uses the Gradle build tool.
* A `grails-app` root sub-directory indicates a Grails application.

## Dependencies

### Maven

When using the Maven build tool, dependencies are added to the `<dependencies>`
node in `pom.xml`.

For example, here is a `pom.xml` file to declare a compile-time dependency on
`log4j`:

```xml
<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <groupId>io.flynn.example</groupId>
  <artifactId>flynn-example</artifactId>
  <version>1.0-SNAPSHOT</version>
  <packaging>jar</packaging>

  <name>flynn-example</name>

  <dependencies>
    <dependency>
      <groupId>log4j</groupId>
      <artifactId>log4j</artifactId>
      <version>1.2.17</version>
      <scope>compile</scope>
    </dependency>
  </dependencies>
</project>
```

When an application containing this file is deployed to Flynn, the following
Maven command is run:

```
mvn -B -DskipTests=true clean install
```

This downloads the necessary dependencies and compiles the application into the
`target` directory.

By default, Flynn uses a recent version of Maven. To use a specific version of
Maven, the `maven.version` property should be set in the `system.properties`
file in the root directory of the application, for example:

```
maven.version=3.2.3
```

*For more information on specifying dependencies with Maven, see the [Maven Dependencies page](http://maven.apache.org/guides/introduction/introduction-to-dependency-mechanism.html).*

### Gradle

When using the Gradle build tool, dependencies are specified in a `build.gradle` file
in the root directory using various dependency configurations provided by the
[Java plugin](http://www.gradle.org/docs/current/userguide/java_plugin.html).

Flynn runs the `stage` task during deployment which should be defined to compile
the application.

For example, here is a `build.gradle` file which declares a compile-time dependency
on `log4j` and defines a `stage` task to perform a clean build of the application:

```groovy
apply plugin: "java"

repositories {
  mavenCentral()
}

dependencies {
  compile group: "log4j", name: "log4j", version: "1.2.17"
}

task stage (dependsOn: ["clean", "jar"])
```

When an application containing this file is deployed to Flynn, the following
Gradle command is run:

```
./gradlew stage
```

This downloads the necessary dependencies and compiles the application into the
`build` directory.

*Note: It is recommended that the application contain the `gradlew` script which determines which version of Gradle to use. If it is not present, Flynn will install one, but then the version is not deterministic. See the [Gradle Wrapper page](http://www.gradle.org/docs/current/userguide/gradle_wrapper.html) for more information.*

*For more information on specifying dependencies with Gradle, see the [Gradle Dependencies page](http://www.gradle.org/docs/current/userguide/artifact_dependencies_tutorial.html).*

### Grails

Grails applications define their dependencies within the `grails.project.dependency.resolution`
setting in the `grails-app/conf/BuildConfig.groovy` file.

For example, the following declares a runtime dependency on `postgresql`:

```groovy
grails.project.dependency.resolution = {
  dependencies {
    runtime group: "org.postgresql", name: "postgresql", version: "9.3-1102-jdbc41"
  }
}
```

When a Grails application is deployed to Flynn, the following commands are run:

```
./grailsw compile --non-interactive
./grailsw war --non-interactive
```

These download the necessary dependencies (including Grails), then compile and
package the application into the `target` directory.

*For more information on specifying dependencies in a Grails application, see the [Grails Dependency Resolution page](http://grails.org/doc/latest/guide/conf.html#dependencyResolution).*

### Java Runtime

By default, Flynn uses OpenJDK 8 to run Java applications. OpenJDK 6 and 7 are
also available, and can be used by setting `java.runtime.version` in a
`system.properties` file in the root directory:

```
! OpenJDK 6
java.runtime.version=1.6

! OpenJDK 7
java.runtime.version=1.7
```

## Process Types

The type of processes that your application supports can be declared in a `Procfile` in the
root directory, which contains a line per type in the format `TYPE: COMMAND`.

### web

The `web` process type gets an allocated HTTP route and a corresponding `PORT` environment
variable, so it typically starts an HTTP server for your application.

#### Embedded Jetty

Applications which embed a Jetty server should start the server using the `PORT`
environment variable, for example:

```java
import javax.servlet.http.HttpServlet;
import org.eclipse.jetty.server.Server;

public class MyServlet extends HttpServlet
{
  // handler definitions

  public static void main(String[] args) throws Exception
  {
      Server server = new Server(Integer.valueOf(System.getenv("PORT")));
      // code to set handlers and start the server
  }
}
```

*For more information on embedding a Jetty server, see the [Embedding Jetty page](https://wiki.eclipse.org/Jetty/Tutorial/Embedding_Jetty).*

Assuming the application is using the Gradle build tool and the [Gradle application
plugin](http://www.gradle.org/docs/current/userguide/application_plugin.html) with an
application name of `example`, the process type is defined in the `Procfile` as:

```
web: build/install/example/bin/example
```

#### Jetty + WAR

Applications which package themselves as WAR files need to be run using an external
Servlet container.

The [Maven Dependency Plugin](http://maven.apache.org/plugins/maven-dependency-plugin/)
can be used to copy a `jetty-runner` JAR file into the `target/dependency` directory
by adding the following to `pom.xml`:

```
<build>
  <plugins>
    <plugin>
      <groupId>org.apache.maven.plugins</groupId>
      <artifactId>maven-dependency-plugin</artifactId>
      <version>2.3</version>
      <executions>
        <execution>
          <phase>package</phase>
          <goals><goal>copy</goal></goals>
          <configuration>
            <artifactItems>
              <artifactItem>
                <groupId>org.mortbay.jetty</groupId>
                <artifactId>jetty-runner</artifactId>
                <version>7.5.4.v20111024</version>
                <destFileName>jetty-runner.jar</destFileName>
              </artifactItem>
            </artifactItems>
          </configuration>
        </execution>
      </executions>
    </plugin>
  </plugins>
</build>
```

Assuming the application is packaged into a WAR file in the `target` directory,
the process type is defined in the `Procfile` as:

```
web: java $JAVA_OPTS -jar target/dependency/jetty-runner.jar --port $PORT target/*.war
```

#### Grails

Flynn installs the [webapp runner](https://github.com/jsimone/webapp-runner)
into a `server` directory when it compiles Grails applications, so can be used
to start the application:

```
web: java $JAVA_OPTS -jar server/webapp-runner.jar --port $PORT target/*.war
```

Grails applications can choose to instead have a Jetty runner installed by setting
`grails.application.container=jetty` in a `system.properties` file in the root
directory, and defining the process type as:

```
web java $JAVA_OPTS -jar server/jetty-runner.jar --port $PORT target/*.war
```
