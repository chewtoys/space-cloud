package com.spaceuptech.dbevents.spacecloud

import akka.actor.typed.scaladsl.{AbstractBehavior, ActorContext, Behaviors, TimerScheduler}
import akka.actor.typed._
import com.spaceuptech.dbevents.Global
import com.spaceuptech.dbevents.database.Database

import scala.concurrent.duration._
import scala.concurrent.{ExecutionContextExecutor, Future}
import scala.util._

object ProjectManager {
  val fetchEventingConfigKey: String = "fetch-eventing-config"
  val fetchDatabasesKey: String = "fetch-databases"

  def apply(projectId: String): Behavior[Command] =
    Behaviors.withTimers(timers => Behaviors.setup(context => new ProjectManager(context, timers, projectId)))

  sealed trait Command

  case class FetchEventingConfig() extends Command

  case class ProcessEventingConfig(config: EventingConfig) extends Command

  case class FetchDatabaseConfig() extends Command

  case class ProcessDatabaseConfig(dbs: Array[DatabaseConfig]) extends Command

  case class Stop() extends Command
}

class ProjectManager(context: ActorContext[ProjectManager.Command], timers: TimerScheduler[ProjectManager.Command], projectId: String) extends AbstractBehavior(context) {

  import ProjectManager._

  // Member variables
  private var databaseToActor: Map[String, ActorRef[Database.Command]] = Map.empty
  private val eventsSink = context.spawn(EventsSink(projectId), "event-sink")
  private var isEventingEnabled = false

  // Start the timer
  timers.startTimerAtFixedRate(fetchEventingConfigKey, FetchEventingConfig(), 1.minute)
  timers.startTimerAtFixedRate(fetchDatabasesKey, FetchDatabaseConfig(), 1.minute)

  println(s"Starting project manager - '$projectId'")

  override def onMessage(msg: Command): Behavior[Command] = {
    msg match {
      case FetchEventingConfig() =>
        println(s"Fetching eventing config for project '$projectId'")
        fetchEventingConfig()
        this

      case ProcessEventingConfig(config) =>
        processEventingConfig(config)
        this

      case FetchDatabaseConfig() =>
        if (isEventingEnabled) {
          println(s"Fetching database config for project '$projectId'")
          fetchDatabaseConfig()
        }
        this

      case ProcessDatabaseConfig(dbs) =>
        processDatabaseConfig(dbs)
        this

      case Stop() =>
        println(s"Got close command for project - $projectId")
        Behaviors.stopped
    }
  }

  private def fetchDatabaseConfig(): Unit = {
    implicit val system: ActorSystem[Nothing] = context.system
    implicit val executionContext: ExecutionContextExecutor = system.executionContext

    val response: Future[DatabaseConfigResponse] = fetchSpaceCloudResource[DatabaseConfigResponse](s"http://${Global.gatewayUrl}/v1/config/projects/$projectId/database/config")
    response.onComplete {
      case Success(value) =>
        if (value.error.isDefined) {
          println(s"Unable to fetch database config for project ($projectId)", value.error.get)
          return
        }

        context.self ! ProcessDatabaseConfig(value.result)
      case Failure(ex) => println(s"Unable to fetch database config for project ($projectId)", ex)
    }
  }

  private def processDatabaseConfig(dbs: Array[DatabaseConfig]): Unit = {
    // Filter all disabled databases
    val filteredDbs: Array[DatabaseConfig] = dbs.filter(db => db.enabled && db.`type` != "sqlserver")

    // Create actor for new projects
    for (db <- filteredDbs) {
      if (!databaseToActor.contains(db.dbAlias)) {
        println(s"Creating new database actor - ${db.dbAlias}")
        val actor = context.spawn(Database.createActor(projectId, db.`type`, eventsSink), s"db-${db.dbAlias}")
        actor ! Database.UpdateEngineConfig(db)
        databaseToActor += db.dbAlias -> actor
      } else {
        // Send update engine command
        databaseToActor.get(db.dbAlias) match {
          case Some(actor) => actor ! Database.UpdateEngineConfig(db)
          case None => // Nothing to be done here
        }
      }
    }

    databaseToActor = databaseToActor.filter(elem => removeDatabaseIfInactive(dbs, elem._1, elem._2))
  }

  private def removeDatabaseIfInactive(dbs: Array[DatabaseConfig], dbAlias: String, actor: ActorRef[Database.Command]): Boolean = {
    if (!dbs.exists(db => db.dbAlias == dbAlias)) {
      println(s"Removing database ($dbAlias) in project ($projectId)")
      actor ! Database.Stop()
      return false
    }
    true
  }

  private def fetchEventingConfig(): Unit = {
    implicit val system: ActorSystem[Nothing] = context.system
    implicit val executionContext: ExecutionContextExecutor = system.executionContext

    val response: Future[EventingConfigResponse] = fetchSpaceCloudResource[EventingConfigResponse](s"http://${Global.gatewayUrl}/v1/config/projects/$projectId/eventing/config")
    response.onComplete {
      case Success(value) =>
        if (value.error.isDefined || value.result.length == 0) {
          println(s"Unable to fetch eventing config for project ($projectId)", value.error.get)
          return
        }

        context.self ! ProcessEventingConfig(value.result(0))
      case Failure(ex) => println(s"Unable to fetch eventing config for project ($projectId)", ex)
    }
  }

  private def processEventingConfig(config: EventingConfig): Unit = {
    // Stop and remove all children if eventing is disabled
    if (!config.enabled) {
      isEventingEnabled = false
      removeAllChildren()
      return
    }

    isEventingEnabled = true

    context.self ! FetchDatabaseConfig()
  }

  private def removeAllChildren(): Unit = {
    for ((_, actor) <- databaseToActor) {
      actor ! Database.Stop()
    }
    databaseToActor = Map.empty
  }

  override def onSignal: PartialFunction[Signal, Behavior[Command]] = {
    case PostStop =>
      timers.cancelAll()
      removeAllChildren()
      eventsSink ! EventsSink.Stop()
      println(s"Closing project manager - '$projectId'")
      this
  }
}
