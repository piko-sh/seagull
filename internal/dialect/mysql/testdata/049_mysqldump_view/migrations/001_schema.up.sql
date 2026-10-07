/*!40101 SET NAMES utf8mb4 */;

CREATE TABLE `boats` (
  `id` int NOT NULL AUTO_INCREMENT,
  `name` varchar(100) NOT NULL,
  `length_m` decimal(6,2) DEFAULT NULL,
  PRIMARY KEY (`id`)
) /*!50100 ENGINE=InnoDB */;

/*!50001 CREATE ALGORITHM=UNDEFINED */
/*!50013 DEFINER=`root`@`localhost` SQL SECURITY DEFINER */
/*!50001 VIEW `long_boats` AS select `boats`.`id` AS `id`,`boats`.`name` AS `name` from `boats` where (`boats`.`length_m` > 20) */;

CREATE ALGORITHM=MERGE DEFINER=`app`@`%` SQL SECURITY INVOKER VIEW `boat_names` AS SELECT `name` FROM `boats`;

CREATE DEFINER=`root`@`%` FUNCTION `boat_label`(`boat_name` varchar(100)) RETURNS varchar(120) DETERMINISTIC NO SQL RETURN CONCAT('Boat ', `boat_name`);
